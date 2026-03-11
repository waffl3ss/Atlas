package middleware

import (
	"database/sql"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// AuthRequired checks if the user is authenticated and verifies the user still exists
func AuthRequired(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")

		if userID == nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		// Verify user still exists in database
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", userID).Scan(&exists)
		if !exists {
			session.Clear()
			session.Save()
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}

		c.Next()
	}
}

// ProjectAccessRequired checks that the authenticated user is a member of the project.
// Must be used AFTER AuthRequired and on routes that have an :id param for the project.
func ProjectAccessRequired(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		if projectID == "" {
			c.Next()
			return
		}

		session := sessions.Default(c)
		userID := session.Get("user_id")

		var exists bool
		err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM project_users WHERE project_id = ? AND user_id = ?)", projectID, userID).Scan(&exists)
		if err != nil || !exists {
			c.HTML(http.StatusForbidden, "error.html", gin.H{"error": "You don't have access to this project"})
			c.Abort()
			return
		}

		c.Next()
	}
}
