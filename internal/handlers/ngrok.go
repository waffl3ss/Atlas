package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	crand "crypto/rand"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	ngrokLib "golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

var (
	ngrokMu       sync.Mutex
	ngrokTunnel   ngrokLib.Tunnel
	ngrokCancel   context.CancelFunc
	ngrokURL      string
	ngrokStarting bool
	ngrokHandler  http.Handler
)

// SetNgrokHandler sets the HTTP handler used when serving via ngrok tunnel.
func SetNgrokHandler(h http.Handler) {
	ngrokHandler = h
}

// IsNgrokActive returns whether the tunnel is running
func IsNgrokActive() bool {
	ngrokMu.Lock()
	defer ngrokMu.Unlock()
	return ngrokTunnel != nil
}

// GetNgrokURL returns the public URL if active
func GetNgrokURL() string {
	ngrokMu.Lock()
	defer ngrokMu.Unlock()
	return ngrokURL
}

func isAdminUser(db *sql.DB, userID interface{}) bool {
	var isAdmin bool
	db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)
	return isAdmin
}

func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "********"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

var encryptionKey string

// SetEncryptionKey sets the key used to encrypt sensitive values in the database.
func SetEncryptionKey(key string) {
	encryptionKey = key
}

func encryptValue(plaintext, key string) (string, error) {
	keyBytes := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(keyBytes[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(crand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptValue(encoded, key string) (string, error) {
	keyBytes := sha256.Sum256([]byte(key))
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(keyBytes[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// NgrokPage renders the ngrok management page
func NgrokPage(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		username := session.Get("username")
		userID := session.Get("user_id")

		if !isAdminUser(db, userID) {
			c.Redirect(http.StatusFound, "/projects")
			return
		}

		var encryptedToken string
		db.QueryRow("SELECT value FROM settings WHERE key = 'ngrok_authtoken'").Scan(&encryptedToken)

		displayToken := encryptedToken
		if encryptionKey != "" && encryptedToken != "" {
			decrypted, err := decryptValue(encryptedToken, encryptionKey)
			if err != nil {
				// Fallback: use encrypted value as-is (for existing unencrypted tokens)
				displayToken = encryptedToken
			} else {
				displayToken = decrypted
			}
		}

		c.HTML(http.StatusOK, "ngrok.html", gin.H{
			"username":     username,
			"is_admin":     true,
			"ngrok_active": IsNgrokActive(),
			"ngrok_url":    GetNgrokURL(),
			"authtoken":    maskToken(displayToken),
			"has_token":    encryptedToken != "",
		})
	}
}

// NgrokSaveToken saves the authtoken
func NgrokSaveToken(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if !isAdminUser(db, userID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Admin only"})
			return
		}

		token := c.PostForm("authtoken")
		if token == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Token required"})
			return
		}

		storeValue := token
		if encryptionKey != "" {
			encrypted, err := encryptValue(token, encryptionKey)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to encrypt token"})
				return
			}
			storeValue = encrypted
		}

		_, err := db.Exec("INSERT INTO settings (key, value) VALUES ('ngrok_authtoken', ?) ON CONFLICT(key) DO UPDATE SET value = ?", storeValue, storeValue)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Failed to save token"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "masked_token": maskToken(token)})
	}
}

// NgrokStart starts the tunnel
func NgrokStart(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if !isAdminUser(db, userID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Admin only"})
			return
		}

		ngrokMu.Lock()
		if ngrokTunnel != nil || ngrokStarting {
			ngrokMu.Unlock()
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "Tunnel already running"})
			return
		}
		ngrokStarting = true
		ngrokMu.Unlock()

		var encryptedToken string
		db.QueryRow("SELECT value FROM settings WHERE key = 'ngrok_authtoken'").Scan(&encryptedToken)
		if encryptedToken == "" {
			ngrokMu.Lock()
			ngrokStarting = false
			ngrokMu.Unlock()
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "No authtoken configured"})
			return
		}

		authtoken := encryptedToken
		if encryptionKey != "" {
			decrypted, err := decryptValue(encryptedToken, encryptionKey)
			if err != nil {
				// Fallback: try using the value as-is (for existing unencrypted tokens)
				authtoken = encryptedToken
			} else {
				authtoken = decrypted
			}
		}

		// Reject API keys — ngrok requires an authtoken for tunnels
		trimmed := strings.TrimSpace(authtoken)
		if strings.HasPrefix(trimmed, "ngrok_api_") {
			ngrokMu.Lock()
			ngrokStarting = false
			ngrokMu.Unlock()
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "That looks like an ngrok API key. Please use your authtoken instead — find it at dashboard.ngrok.com/get-started/your-authtoken"})
			return
		}

		// Create the tunnel context — this stays alive for the tunnel's lifetime
		tunnelCtx, tunnelCancel := context.WithCancel(context.Background())

		// Use a goroutine-based timeout for the initial connection attempt
		type listenResult struct {
			tunnel ngrokLib.Tunnel
			err    error
		}
		resultCh := make(chan listenResult, 1)
		go func() {
			t, err := ngrokLib.Listen(tunnelCtx,
				config.HTTPEndpoint(),
				ngrokLib.WithAuthtoken(authtoken),
			)
			resultCh <- listenResult{t, err}
		}()

		// Wait for connection with a timeout
		var tunnel ngrokLib.Tunnel
		select {
		case result := <-resultCh:
			if result.err != nil {
				tunnelCancel()
				ngrokMu.Lock()
				ngrokStarting = false
				ngrokMu.Unlock()
				log.Printf("Ngrok start error: %v", result.err)
				errMsg := "Failed to start tunnel"
				errStr := result.err.Error()
				if strings.Contains(errStr, "ERR_NGROK_105") || strings.Contains(errStr, "invalid authtoken") || strings.Contains(errStr, "authentication failed") {
					errMsg = "Invalid authtoken. Make sure you're using your authtoken (not an API key) from dashboard.ngrok.com/get-started/your-authtoken"
				}
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": errMsg})
				return
			}
			tunnel = result.tunnel
		case <-time.After(15 * time.Second):
			tunnelCancel()
			ngrokMu.Lock()
			ngrokStarting = false
			ngrokMu.Unlock()
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Tunnel connection timed out. Check your authtoken and network connection."})
			return
		}

		tunnelURL := tunnel.URL()
		if tunnelURL == "" {
			tunnel.Close()
			tunnelCancel()
			ngrokMu.Lock()
			ngrokStarting = false
			ngrokMu.Unlock()
			log.Printf("Ngrok tunnel established but returned empty URL")
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Tunnel started but no public URL was returned. Check your authtoken."})
			return
		}

		log.Printf("Ngrok tunnel established: %s", tunnelURL)

		ngrokMu.Lock()
		ngrokTunnel = tunnel
		ngrokCancel = tunnelCancel
		ngrokURL = tunnelURL
		ngrokStarting = false
		ngrokMu.Unlock()

		// Serve the app directly on the ngrok tunnel, clean up on failure
		go func() {
			log.Printf("Ngrok serving handler on tunnel")
			if err := http.Serve(tunnel, ngrokHandler); err != nil {
				log.Printf("Ngrok serve ended: %v", err)
			}
			// Tunnel stopped — clean up state so the UI reflects reality
			ngrokMu.Lock()
			if ngrokTunnel == tunnel {
				ngrokTunnel = nil
				ngrokCancel = nil
				ngrokURL = ""
				log.Printf("Ngrok tunnel cleaned up after unexpected stop")
			}
			ngrokMu.Unlock()
		}()

		c.JSON(http.StatusOK, gin.H{"success": true, "url": tunnelURL})
	}
}

// NgrokStop stops the tunnel
func NgrokStop(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")
		if !isAdminUser(db, userID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Admin only"})
			return
		}

		ngrokMu.Lock()
		defer ngrokMu.Unlock()

		if ngrokTunnel == nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "error": "Tunnel not running"})
			return
		}

		ngrokTunnel.Close()
		if ngrokCancel != nil {
			ngrokCancel()
		}
		ngrokTunnel = nil
		ngrokCancel = nil
		ngrokURL = ""

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// NgrokStatus returns current tunnel status
func NgrokStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"active": IsNgrokActive(),
			"url":    GetNgrokURL(),
		})
	}
}
