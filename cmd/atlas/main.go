package main

import (
	"atlas/internal/certs"
	"atlas/internal/database"
	"atlas/internal/handlers"
	"atlas/internal/middleware"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

// Version is set at build time via -ldflags "-X main.Version=..."
var Version = "dev"

// getAtlasDir returns the path to ~/.atlas, creating it if needed.
func getAtlasDir() (string, error) {
	var homeDir string
	var err error

	if runtime.GOOS == "windows" {
		homeDir = os.Getenv("USERPROFILE")
	} else {
		homeDir = os.Getenv("HOME")
	}

	if homeDir == "" {
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	atlasDir := filepath.Join(homeDir, ".atlas")
	if err := os.MkdirAll(atlasDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create .atlas directory: %w", err)
	}

	return atlasDir, nil
}

// getOrCreateSessionKey reads the session key from the atlas directory,
// or generates a new one if it doesn't exist.
func getOrCreateSessionKey(atlasDir string) (string, error) {
	keyPath := filepath.Join(atlasDir, "session.key")

	// Try to read existing key
	data, err := os.ReadFile(keyPath)
	if err == nil {
		key := strings.TrimSpace(string(data))
		if key != "" {
			return key, nil
		}
	}

	// Generate new key: 32 random bytes, hex-encoded
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random session key: %w", err)
	}
	key := hex.EncodeToString(randomBytes)

	if err := os.WriteFile(keyPath, []byte(key), 0600); err != nil {
		return "", fmt.Errorf("failed to write session key file: %w", err)
	}

	return key, nil
}

func main() {
	// Initialize database
	db, err := database.Initialize()
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	// Get atlas directory
	atlasDir, err := getAtlasDir()
	if err != nil {
		log.Fatal("Failed to get atlas directory:", err)
	}

	// Generate or load session secret
	sessionKey, err := getOrCreateSessionKey(atlasDir)
	if err != nil {
		log.Fatal("Failed to get session key:", err)
	}

	// Set encryption key for sensitive data storage
	handlers.SetEncryptionKey(sessionKey)

	// TLS certificates: use custom paths from env vars, or auto-generate
	certPath := os.Getenv("ATLAS_TLS_CERT")
	keyPath := os.Getenv("ATLAS_TLS_KEY")
	if certPath != "" && keyPath != "" {
		log.Printf("Using custom TLS certificates: %s, %s", certPath, keyPath)
	} else {
		certPath, keyPath, err = certs.EnsureCertificates(atlasDir)
		if err != nil {
			log.Fatal("Failed to ensure TLS certificates:", err)
		}
		log.Printf("Using auto-generated TLS certificates: %s, %s", certPath, keyPath)
	}

	// Set Gin mode
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.SetTrustedProxies(nil)

	// Load templates - need to load from both directories
	templates := []string{}
	rootTemplates, _ := filepath.Glob("templates/*.html")
	includeTemplates, _ := filepath.Glob("templates/includes/*.html")
	templates = append(templates, rootTemplates...)
	templates = append(templates, includeTemplates...)
	r.LoadHTMLFiles(templates...)
	r.Static("/static", "./static")

	// Session middleware
	store := cookie.NewStore([]byte(sessionKey))
	r.Use(sessions.Sessions("atlas_session", store))

	// Public routes
	r.GET("/login", handlers.LoginPage)
	r.POST("/login", handlers.Login(db))
	r.GET("/logout", handlers.Logout)

	// Protected routes (require authentication)
	protected := r.Group("/")
	protected.Use(middleware.AuthRequired(db))
	{
		protected.GET("/", handlers.ProjectList(db))
		protected.GET("/projects", handlers.ProjectList(db))
		protected.POST("/projects/create", handlers.CreateProject(db))

		// Admin routes
		protected.GET("/admin/users", handlers.AdminUsers(db))
		protected.POST("/admin/users/create", handlers.CreateUser(db))
		protected.POST("/admin/users/delete", handlers.DeleteUser(db))
		protected.POST("/admin/users/change-password", handlers.ChangePassword(db))
		protected.POST("/admin/reset-database", handlers.ResetDatabase(db))
		protected.GET("/admin/ngrok", handlers.NgrokPage(db))
		protected.POST("/admin/ngrok/save-token", handlers.NgrokSaveToken(db))
		protected.POST("/admin/ngrok/start", handlers.NgrokStart(db))
		protected.POST("/admin/ngrok/stop", handlers.NgrokStop(db))
		protected.GET("/admin/ngrok/status", handlers.NgrokStatus())
	}

	// Project routes (require authentication + project membership)
	project := r.Group("/projects/:id")
	project.Use(middleware.AuthRequired(db))
	project.Use(middleware.ProjectAccessRequired(db))
	{
		project.GET("", handlers.ProjectDashboard(db))
		project.GET("/hosts", handlers.ProjectHosts(db))
		project.POST("/hosts/delete", handlers.DeleteHost(db))
		project.POST("/hosts/bulk-delete", handlers.BulkDeleteHosts(db))
		project.POST("/hosts/color", handlers.UpdateHostColor(db))
		project.POST("/hosts/bulk-color", handlers.BulkUpdateHostColor(db))
		project.POST("/hosts/add", handlers.AddHost(db))
		project.POST("/hosts/bulk-add", handlers.BulkAddHosts(db))
		project.GET("/hosts/:host_id", handlers.HostDetail(db))
		project.POST("/hosts/:host_id/update", handlers.UpdateHostInfo(db))
		project.POST("/hosts/:host_id/services/add", handlers.AddHostService(db))
		project.POST("/hosts/:host_id/services/delete", handlers.DeleteHostService(db))
		project.POST("/hosts/:host_id/services/bulk-delete", handlers.BulkDeleteHostServices(db))
		project.POST("/hosts/:host_id/services/color", handlers.UpdateServiceColor(db))
		project.POST("/hosts/:host_id/hostnames/add", handlers.AddHostHostname(db))
		project.POST("/hosts/:host_id/hostnames/delete", handlers.DeleteHostHostname(db))
		project.POST("/hosts/:host_id/hostnames/bulk-delete", handlers.BulkDeleteHostHostnames(db))
		project.POST("/hosts/:host_id/hostnames/set-default", handlers.SetDefaultHostname(db))
		project.POST("/hosts/:host_id/webdirs/add", handlers.AddWebDirectory(db))
		project.POST("/hosts/:host_id/webdirs/delete", handlers.DeleteWebDirectory(db))
		project.POST("/hosts/:host_id/webdirs/bulk-delete", handlers.BulkDeleteWebDirectories(db))
		project.POST("/hosts/:host_id/webprobes/add", handlers.AddWebProbe(db))
		project.POST("/hosts/:host_id/webprobes/delete", handlers.DeleteWebProbe(db))
		project.POST("/hosts/:host_id/webprobes/bulk-delete", handlers.BulkDeleteWebProbes(db))
		project.GET("/hosts/:host_id/services/json", handlers.GetHostServicesJSON(db))
		project.POST("/hosts/:host_id/findings/remove", handlers.RemoveHostFindingAssoc(db))
		project.POST("/hosts/:host_id/findings/bulk-remove", handlers.BulkRemoveHostFindingAssocs(db))
		project.GET("/services", handlers.ProjectServices(db))
		project.GET("/services/hosts", handlers.GetServiceHosts(db))
		project.POST("/services/delete", handlers.DeleteService(db))
		project.POST("/services/bulk-delete", handlers.BulkDeleteServices(db))
		project.POST("/services/merge", handlers.MergeServices(db))
		project.GET("/findings", handlers.ProjectFindings(db))
		project.POST("/findings/add", handlers.AddFinding(db))
		project.POST("/findings/delete", handlers.DeleteFinding(db))
		project.POST("/findings/bulk-delete", handlers.BulkDeleteFindings(db))
		project.POST("/findings/color", handlers.UpdateFindingColor(db))
		project.POST("/findings/bulk-color", handlers.BulkUpdateFindingColor(db))
		project.GET("/findings/:fid", handlers.FindingDetail(db))
		project.POST("/findings/:fid/update", handlers.UpdateFinding(db))
		project.POST("/findings/:fid/delete", handlers.DeleteFindingFromDetail(db))
		project.POST("/findings/:fid/cves/add", handlers.AddFindingCVE(db))
		project.POST("/findings/:fid/cves/delete", handlers.DeleteFindingCVE(db))
		project.POST("/findings/:fid/cves/bulk-delete", handlers.BulkDeleteFindingCVEs(db))
		project.POST("/findings/:fid/hosts/add", handlers.AddFindingHost(db))
		project.POST("/findings/:fid/hosts/delete", handlers.DeleteFindingHost(db))
		project.POST("/findings/:fid/hosts/bulk-delete", handlers.BulkDeleteFindingHosts(db))
		project.POST("/findings/:fid/hosts/bulk-add", handlers.BulkAddFindingHosts(db))
		project.GET("/credentials", handlers.ProjectCredentials(db))
		project.GET("/users", handlers.ProjectUsers(db))
		project.GET("/uploads", handlers.ProjectUploads(db))
		project.GET("/exports", handlers.ProjectExports(db))
		project.GET("/settings", handlers.ProjectSettings(db))
		project.POST("/add-user", handlers.AddUserToProject(db))
		project.POST("/remove-user", handlers.RemoveUserFromProject(db))
		project.POST("/delete", handlers.DeleteProject(db))
		project.POST("/credentials/add", handlers.AddCredential(db))
		project.POST("/credentials/delete", handlers.DeleteCredential(db))
		project.POST("/credentials/bulk-delete", handlers.BulkDeleteCredentials(db))
		project.GET("/credentials/export", handlers.ExportCredentials(db))
		project.POST("/users/add", handlers.AddDiscoveredUser(db))
		project.POST("/users/bulk", handlers.BulkAddDiscoveredUsers(db))
		project.POST("/users/upload", handlers.UploadDiscoveredUsers(db))
		project.POST("/users/delete", handlers.DeleteDiscoveredUser(db))
		project.POST("/users/bulk-delete", handlers.BulkDeleteDiscoveredUsers(db))
		project.GET("/users/export", handlers.ExportDiscoveredUsers(db))
		project.POST("/upload", handlers.UploadFile(db))
		project.POST("/upload/delete", handlers.DeleteUpload(db))
		project.POST("/exports/delete", handlers.DeleteExport(db))
		project.GET("/exports/:export_id/download", handlers.DownloadExport(db))
		project.POST("/exports/generate-raw", handlers.GenerateRawExport(db))
		project.POST("/exports/generate-plextrac-assets", handlers.GeneratePlexTracAssets(db))
		project.POST("/exports/generate-plextrac-findings", handlers.GeneratePlexTracFindings(db))
		project.POST("/exports/generate-lair", handlers.GenerateLairExport(db))
		project.POST("/exports/tags/add", handlers.AddExportTag(db))
		project.POST("/exports/tags/delete", handlers.DeleteExportTag(db))
	}

	// Set the handler for ngrok tunnel serving
	handlers.SetNgrokHandler(r)

	// Start HTTPS server on :8443
	log.Printf("Starting Atlas %s on https://localhost:8443", Version)
	if err := r.RunTLS(":8443", certPath, keyPath); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
