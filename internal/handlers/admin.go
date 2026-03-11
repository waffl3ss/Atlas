package handlers

import (
	"database/sql"
	"atlas/internal/database"
	"atlas/internal/models"
	"log"
	"net/http"
	"unicode"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func validatePassword(password string) string {
	if len(password) < 10 {
		return "Password must be at least 10 characters"
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, ch := range password {
		switch {
		case unicode.IsUpper(ch):
			hasUpper = true
		case unicode.IsLower(ch):
			hasLower = true
		case unicode.IsDigit(ch):
			hasDigit = true
		case unicode.IsPunct(ch) || unicode.IsSymbol(ch):
			hasSpecial = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit || !hasSpecial {
		return "Password must contain uppercase, lowercase, number, and special character"
	}
	return ""
}

// AdminUsers shows the user management page
func AdminUsers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		username := session.Get("username")
		userID := session.Get("user_id")

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		if !isAdmin {
			c.HTML(http.StatusForbidden, "error.html", gin.H{
				"error": "Access denied. Admin privileges required.",
			})
			return
		}

		// Get all users
		rows, err := db.Query("SELECT id, username, created_at FROM users ORDER BY id ASC")
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{
				"error": "Failed to load users",
			})
			return
		}
		defer rows.Close()

		var users []models.User
		for rows.Next() {
			var u models.User
			rows.Scan(&u.ID, &u.Username, &u.CreatedAt)
			users = append(users, u)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		c.HTML(http.StatusOK, "admin_users.html", gin.H{
			"username":     username,
			"is_admin":     isAdmin,
			"users":        users,
			"ngrok_active": IsNgrokActive(),
		})
	}
}

// CreateUser handles user creation
func CreateUser(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}

		username := c.PostForm("username")
		password := c.PostForm("password")

		if username == "" || password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username and password are required"})
			return
		}

		if len(username) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username must be at least 2 characters"})
			return
		}

		if msg := validatePassword(password); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}

		// Check if username already exists
		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE username = ?)", username).Scan(&exists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check username"})
			return
		}

		if exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username already exists"})
			return
		}

		// Hash password
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}

		// Create user
		_, err = db.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", username, string(hash))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// DeleteUser handles user deletion
func DeleteUser(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}

		deleteUserID := c.PostForm("user_id")

		if deleteUserID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
			return
		}

		// Check if trying to delete admin
		var username string
		err := db.QueryRow("SELECT username FROM users WHERE id = ?", deleteUserID).Scan(&username)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
			return
		}

		if username == "admin" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete the admin user"})
			return
		}

		// Delete user
		_, err = db.Exec("DELETE FROM users WHERE id = ?", deleteUserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// ChangePassword handles password changes
func ChangePassword(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}

		changeUserID := c.PostForm("user_id")
		newPassword := c.PostForm("password")

		if changeUserID == "" || newPassword == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User ID and password are required"})
			return
		}

		if msg := validatePassword(newPassword); msg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}

		// Hash new password
		hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}

		// Update password
		_, err = db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(hash), changeUserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update password"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// ResetDatabase drops all tables and recreates the schema
func ResetDatabase(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		if !isAdmin {
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			return
		}

		// Clear the session (log out the user)
		session.Clear()
		session.Save()

		// Reset the database schema
		err := database.ResetSchema(db)
		if err != nil {
			log.Printf("Failed to reset database: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset database"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"redirect": true,
			"message":  "Database reset successful. Redirecting to login...",
		})
	}
}
