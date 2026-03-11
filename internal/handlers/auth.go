package handlers

import (
	"database/sql"
	"net/http"
	"sync"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

var (
	loginAttempts = make(map[string]*loginTracker)
	loginMu       sync.Mutex
)

type loginTracker struct {
	count   int
	firstAt time.Time
}

func init() {
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			loginMu.Lock()
			for ip, tracker := range loginAttempts {
				if time.Since(tracker.firstAt) > 30*time.Minute {
					delete(loginAttempts, ip)
				}
			}
			loginMu.Unlock()
		}
	}()
}

func isRateLimited(ip string) bool {
	loginMu.Lock()
	defer loginMu.Unlock()
	tracker, exists := loginAttempts[ip]
	if !exists {
		return false
	}
	if time.Since(tracker.firstAt) > 30*time.Minute {
		delete(loginAttempts, ip)
		return false
	}
	return tracker.count >= 5
}

func recordFailedLogin(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	tracker, exists := loginAttempts[ip]
	if !exists || time.Since(tracker.firstAt) > 30*time.Minute {
		loginAttempts[ip] = &loginTracker{count: 1, firstAt: time.Now()}
		return
	}
	tracker.count++
}

func resetLoginAttempts(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginAttempts, ip)
}

// getUserID safely extracts the user ID from the session, handling type variations.
func getUserID(c *gin.Context) (int, bool) {
	session := sessions.Default(c)
	userID := session.Get("user_id")
	if userID == nil {
		return 0, false
	}
	switch v := userID.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

// LoginPage renders the login page
func LoginPage(c *gin.Context) {
	session := sessions.Default(c)
	userID := session.Get("user_id")

	// If already logged in, redirect to projects
	if userID != nil {
		c.Redirect(http.StatusFound, "/projects")
		return
	}

	c.HTML(http.StatusOK, "login.html", gin.H{
		"error": c.Query("error"),
	})
}

// Login handles the login POST request
func Login(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if isRateLimited(ip) {
			c.Redirect(http.StatusFound, "/login?error=Too+many+login+attempts.+Try+again+in+30+minutes.")
			return
		}

		username := c.PostForm("username")
		password := c.PostForm("password")

		if username == "" || password == "" {
			c.Redirect(http.StatusFound, "/login?error=Please+enter+username+and+password")
			return
		}

		// Get user from database
		var userID int
		var passwordHash string
		err := db.QueryRow("SELECT id, password_hash FROM users WHERE username = ?", username).Scan(&userID, &passwordHash)
		if err != nil {
			if err == sql.ErrNoRows {
				recordFailedLogin(ip)
				// Dummy bcrypt to prevent timing-based username enumeration
				bcrypt.CompareHashAndPassword([]byte("$2a$10$0000000000000000000000uAyOHtkVHMYJGS.0RjLUXJXpKPxRS6e"), []byte(password))
				c.Redirect(http.StatusFound, "/login?error=Invalid+username+or+password")
				return
			}
			c.Redirect(http.StatusFound, "/login?error=An+error+occurred")
			return
		}

		// Check password
		if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
			recordFailedLogin(ip)
			c.Redirect(http.StatusFound, "/login?error=Invalid+username+or+password")
			return
		}

		// Set session
		session := sessions.Default(c)
		session.Set("user_id", userID)
		session.Set("username", username)
		if err := session.Save(); err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": "Failed to save session"})
			return
		}

		resetLoginAttempts(ip)

		c.Redirect(http.StatusFound, "/projects")
	}
}

// Logout handles user logout
func Logout(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()
	c.Redirect(http.StatusFound, "/login")
}
