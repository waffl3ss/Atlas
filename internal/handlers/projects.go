package handlers

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"atlas/internal/models"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// ipToSortKey converts an IPv4 address to a numeric value for proper sorting.
func ipToSortKey(ip string) int64 {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return 0
	}
	var result int64
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		result = result*256 + int64(n)
	}
	return result
}

// cvssToSeverity derives a severity string from a CVSS score.
// 9.0-10.0 = critical, 7.0-8.9 = high, 4.0-6.9 = medium, 0.1-3.9 = low, 0 = informational
func cvssToSeverity(score float64) string {
	switch {
	case score >= 9.0:
		return "critical"
	case score >= 7.0:
		return "high"
	case score >= 4.0:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "informational"
	}
}

// autoColorNewHosts transitions yellow (untouched) hosts to grey once they have services or findings.
// Yellow is the default for newly created hosts with no data. Once data arrives, they go to grey.
func autoColorNewHosts(db *sql.DB, projectID string) {
	db.Exec(`UPDATE hosts SET color = 'grey' WHERE project_id = ? AND color = 'yellow'
		AND (EXISTS(SELECT 1 FROM services WHERE services.host_id = hosts.id)
		  OR EXISTS(SELECT 1 FROM finding_hosts WHERE finding_hosts.host_id = hosts.id))`, projectID)
}

// verifyHostInProject checks that a host belongs to the given project.
func verifyHostInProject(db *sql.DB, hostID, projectID string) bool {
	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM hosts WHERE id = ? AND project_id = ?)", hostID, projectID).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// ProjectList shows all projects
func ProjectList(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		// Get projects user has access to
		rows, err := db.Query(`
			SELECT p.id, p.name, p.description, p.start_date, p.end_date, p.created_at, p.updated_at
			FROM projects p
			INNER JOIN project_users pu ON p.id = pu.project_id
			WHERE pu.user_id = ?
			ORDER BY p.created_at DESC
		`, userID)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{
				"error": "Failed to load projects",
			})
			return
		}
		defer rows.Close()

		var projects []models.Project
		for rows.Next() {
			var p models.Project
			var startDate, endDate sql.NullString
			err := rows.Scan(&p.ID, &p.Name, &p.Description, &startDate, &endDate, &p.CreatedAt, &p.UpdatedAt)
			if err != nil {
				continue
			}
			if startDate.Valid {
				p.StartDate = startDate.String
			}
			if endDate.Valid {
				p.EndDate = endDate.String
			}
			projects = append(projects, p)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		c.HTML(http.StatusOK, "projects.html", gin.H{
			"username":     username,
			"projects":     projects,
			"is_admin":     isAdmin,
			"ngrok_active": IsNgrokActive(),
		})
	}
}

// CreateProject handles project creation
func CreateProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.PostForm("name")
		description := c.PostForm("description")
		startDate := c.PostForm("start_date")
		endDate := c.PostForm("end_date")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
			return
		}

		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project name is required"})
			return
		}

		// Generate project ID
		projectID, err := models.GenerateProjectID()
		if err != nil {
			fmt.Printf("Error generating project ID: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate project ID"})
			return
		}

		// Insert project
		_, err = db.Exec(`
			INSERT INTO projects (id, name, description, start_date, end_date, creator_id)
			VALUES (?, ?, ?, ?, ?, ?)
		`, projectID, name, description, nullString(startDate), nullString(endDate), userID)

		if err != nil {
			fmt.Printf("Error creating project: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create project"})
			return
		}

		// Add creator to project_users
		_, err = db.Exec(`
			INSERT INTO project_users (project_id, user_id, role)
			VALUES (?, ?, 'owner')
		`, projectID, userID)

		if err != nil {
			fmt.Printf("Error adding creator to project_users: %v\n", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create project"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"project_id": projectID,
		})
	}
}

// ProjectDashboard shows the project dashboard with statistics
func ProjectDashboard(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		// Get project details
		var project models.Project
		var startDate, endDate sql.NullString
		err := db.QueryRow(`
			SELECT id, name, description, start_date, end_date, creator_id, created_at, updated_at
			FROM projects WHERE id = ?
		`, projectID).Scan(&project.ID, &project.Name, &project.Description, &startDate, &endDate, &project.CreatorID, &project.CreatedAt, &project.UpdatedAt)

		if err != nil {
			c.HTML(http.StatusNotFound, "error.html", gin.H{
				"error": "Project not found",
			})
			return
		}

		if startDate.Valid {
			project.StartDate = startDate.String
		}
		if endDate.Valid {
			project.EndDate = endDate.String
		}

		// Get statistics
		stats := models.DashboardStats{
			FindingsBySev: make(map[string]int),
		}

		// Total hosts
		db.QueryRow("SELECT COUNT(*) FROM hosts WHERE project_id = ?", projectID).Scan(&stats.TotalHosts)

		// Total unique services (same grouping as services page: port+protocol+service_name+version)
		db.QueryRow(`SELECT COUNT(*) FROM (
			SELECT DISTINCT port, protocol, service_name, version
			FROM services WHERE project_id = ?
		)`, projectID).Scan(&stats.TotalServices)

		// Total findings
		db.QueryRow("SELECT COUNT(*) FROM findings WHERE project_id = ?", projectID).Scan(&stats.TotalFindings)

		// Findings by severity
		rows, err := db.Query(`
			SELECT severity, COUNT(*)
			FROM findings
			WHERE project_id = ?
			GROUP BY severity
		`, projectID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var severity string
				var count int
				if err := rows.Scan(&severity, &count); err == nil {
					stats.FindingsBySev[severity] = count
				}
			}
			if err := rows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Ensure all severity levels exist in the map
		for _, sev := range []string{"critical", "high", "medium", "low", "informational"} {
			if _, exists := stats.FindingsBySev[sev]; !exists {
				stats.FindingsBySev[sev] = 0
			}
		}

		// Total credentials
		db.QueryRow("SELECT COUNT(*) FROM credentials WHERE project_id = ?", projectID).Scan(&stats.TotalCredentials)

		// Total discovered users
		db.QueryRow("SELECT COUNT(*) FROM discovered_users WHERE project_id = ?", projectID).Scan(&stats.TotalUsers)

		c.HTML(http.StatusOK, "dashboard.html", gin.H{
			"username":     username,
			"project":      project,
			"stats":        stats,
			"is_admin":     isAdmin,
			"page_type":    "dashboard",
			"ngrok_active": IsNgrokActive(),
		})
	}
}

// Placeholder handlers for other pages
func ProjectHosts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		rows, err := db.Query(`
			SELECT h.id, h.ip_address, h.hostname, h.os, h.notes,
				   COALESCE(h.color, 'grey'), COALESCE(h.source, 'manual'),
				   COALESCE(u.username, '') as modified_by_username
			FROM hosts h
			LEFT JOIN users u ON h.last_modified_by = u.id
			WHERE h.project_id = ?
		`, projectID)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": "Failed to load hosts"})
			return
		}
		defer rows.Close()

		var hosts []models.HostWithUser
		for rows.Next() {
			var h models.HostWithUser
			var hostname, os, notes sql.NullString
			err := rows.Scan(&h.ID, &h.IPAddress, &hostname, &os, &notes,
				&h.Color, &h.Source, &h.ModifiedByUsername)
			if err != nil {
				fmt.Println("Host scan error:", err)
				continue
			}
			h.Hostname = hostname.String
			h.OS = os.String
			h.Notes = notes.String
			h.ProjectID = projectID
			hosts = append(hosts, h)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		sort.Slice(hosts, func(i, j int) bool {
			return ipToSortKey(hosts[i].IPAddress) < ipToSortKey(hosts[j].IPAddress)
		})

		c.HTML(http.StatusOK, "hosts.html", gin.H{
			"username":     username,
			"project":      project,
			"page_type":    "hosts",
			"is_admin":     isAdmin,
			"hosts":        hosts,
			"ngrok_active": IsNgrokActive(),
		})
	}
}

func DeleteHost(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.PostForm("host_id")

		_, err := db.Exec("DELETE FROM hosts WHERE id = ? AND project_id = ?", hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete host"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkDeleteHosts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM hosts WHERE id = ? AND project_id = ?", id, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

func UpdateHostColor(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.PostForm("host_id")
		color := c.PostForm("color")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		validColors := map[string]bool{
			"grey": true, "green": true, "blue": true,
			"yellow": true, "orange": true, "red": true,
		}
		if !validColors[color] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid color"})
			return
		}

		_, err := db.Exec(
			"UPDATE hosts SET color = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?",
			color, userID, hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update color"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkUpdateHostColor(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		idsStr := c.PostForm("ids")
		color := c.PostForm("color")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		validColors := map[string]bool{
			"grey": true, "green": true, "blue": true,
			"yellow": true, "orange": true, "red": true,
		}
		if !validColors[color] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid color"})
			return
		}

		ids := strings.Split(idsStr, ",")
		updated := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec(
				"UPDATE hosts SET color = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?",
				color, userID, id, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					updated++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "updated": updated})
	}
}

func AddHost(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		ipAddress := strings.TrimSpace(c.PostForm("ip_address"))

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		if ipAddress == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "IP address is required"})
			return
		}

		// Check for duplicate
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM hosts WHERE project_id = ? AND ip_address = ?)", projectID, ipAddress).Scan(&exists)
		if exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Host already exists in this project"})
			return
		}

		_, err := db.Exec("INSERT INTO hosts (project_id, ip_address, color, source, last_modified_by) VALUES (?, ?, 'yellow', 'manual', ?)",
			projectID, ipAddress, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add host"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkAddHosts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		ipAddresses := c.PostForm("ip_addresses")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		lines := strings.Split(ipAddresses, "\n")
		added := 0
		skipped := 0
		for _, line := range lines {
			ip := strings.TrimSpace(line)
			if ip == "" {
				continue
			}

			var exists bool
			db.QueryRow("SELECT EXISTS(SELECT 1 FROM hosts WHERE project_id = ? AND ip_address = ?)", projectID, ip).Scan(&exists)
			if exists {
				skipped++
				continue
			}

			_, err := db.Exec("INSERT INTO hosts (project_id, ip_address, color, source, last_modified_by) VALUES (?, ?, 'yellow', 'manual', ?)",
				projectID, ip, userID)
			if err != nil {
				skipped++
				continue
			}
			added++
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "added": added, "skipped": skipped})
	}
}

func HostDetail(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		var h models.HostWithUser
		var hostname, os, notes, macAddress, tag sql.NullString
		err := db.QueryRow(`
			SELECT h.id, h.ip_address, h.hostname, h.os, h.notes,
				   COALESCE(h.color, 'grey'), COALESCE(h.source, 'manual'),
				   COALESCE(h.mac_address, ''), COALESCE(h.tag, ''),
				   COALESCE(u.username, '') as modified_by_username
			FROM hosts h
			LEFT JOIN users u ON h.last_modified_by = u.id
			WHERE h.id = ? AND h.project_id = ?
		`, hostID, projectID).Scan(&h.ID, &h.IPAddress, &hostname, &os, &notes,
			&h.Color, &h.Source, &macAddress, &tag, &h.ModifiedByUsername)
		if err != nil {
			c.HTML(http.StatusNotFound, "error.html", gin.H{"error": "Host not found"})
			return
		}
		h.Hostname = hostname.String
		h.OS = os.String
		h.Notes = notes.String
		h.MACAddress = macAddress.String
		h.Tag = tag.String
		h.ProjectID = projectID

		// Query services for this host, deduplicated by port+protocol (keep richest data)
		var services []models.Service
		svcRows, svcErr := db.Query(`
			SELECT id, port, protocol, service_name, version, COALESCE(color, 'grey'), COALESCE(source, 'manual')
			FROM services
			WHERE host_id = ? AND project_id = ?
			AND id IN (
				SELECT id FROM services s1
				WHERE s1.host_id = ? AND s1.project_id = ?
				AND s1.id = (
					SELECT s2.id FROM services s2
					WHERE s2.host_id = s1.host_id AND s2.port = s1.port AND s2.protocol = s1.protocol
					ORDER BY LENGTH(COALESCE(s2.service_name, '')) + LENGTH(COALESCE(s2.version, '')) DESC, s2.id ASC
					LIMIT 1
				)
			)
			ORDER BY port ASC
		`, hostID, projectID, hostID, projectID)
		if svcErr == nil {
			defer svcRows.Close()
			for svcRows.Next() {
				var svc models.Service
				var protocol, serviceName, version sql.NullString
				svcRows.Scan(&svc.ID, &svc.Port, &protocol, &serviceName, &version, &svc.Color, &svc.Source)
				svc.Protocol = protocol.String
				svc.ServiceName = serviceName.String
				svc.Version = version.String
				services = append(services, svc)
			}
			if err := svcRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query hostnames for this host
		var hostnames []models.HostnameEntry
		hnRows, hnErr := db.Query(`
			SELECT id, hostname, COALESCE(source, 'manual')
			FROM hostnames
			WHERE host_id = ? AND project_id = ?
			ORDER BY hostname ASC
		`, hostID, projectID)
		if hnErr == nil {
			defer hnRows.Close()
			for hnRows.Next() {
				var hn models.HostnameEntry
				hnRows.Scan(&hn.ID, &hn.Hostname, &hn.Source)
				hostnames = append(hostnames, hn)
			}
			if err := hnRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// If host.hostname doesn't match any hostname entry, sync it to the first one
		if len(hostnames) > 0 {
			matched := false
			for _, hn := range hostnames {
				if hn.Hostname == h.Hostname {
					matched = true
					break
				}
			}
			if !matched {
				h.Hostname = hostnames[0].Hostname
				db.Exec("UPDATE hosts SET hostname = ? WHERE id = ? AND project_id = ?", hostnames[0].Hostname, hostID, projectID)
			}
		}

		// Query credentials matching this host
		var hostCreds []models.Credential
		credRows, credErr := db.Query("SELECT id, username, password, credential_type, host, service, notes, created_at FROM credentials WHERE project_id = ? ORDER BY username ASC", projectID)
		if credErr == nil {
			defer credRows.Close()
			// Build match set: IP + all hostnames (lowercased)
			matchSet := map[string]bool{strings.ToLower(h.IPAddress): true}
			for _, hn := range hostnames {
				matchSet[strings.ToLower(hn.Hostname)] = true
			}
			for credRows.Next() {
				var cr models.Credential
				if err := credRows.Scan(&cr.ID, &cr.Username, &cr.Password, &cr.CredentialType, &cr.Host, &cr.Service, &cr.Notes, &cr.CreatedAt); err != nil {
					continue
				}
				cr.ProjectID = projectID
				normalized := normalizeCredHost(cr.Host)
				if normalized != "" && matchSet[normalized] {
					hostCreds = append(hostCreds, cr)
				}
			}
			if err := credRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query web directories for this host
		var webDirs []models.WebDirectory
		wdRows, wdErr := db.Query(`
			SELECT id, port, COALESCE(base_domain, ''), path, COALESCE(source, 'manual')
			FROM web_directories
			WHERE host_id = ? AND project_id = ?
			ORDER BY port ASC, base_domain ASC, path ASC
		`, hostID, projectID)
		if wdErr == nil {
			defer wdRows.Close()
			for wdRows.Next() {
				var wd models.WebDirectory
				wdRows.Scan(&wd.ID, &wd.Port, &wd.BaseDomain, &wd.Path, &wd.Source)
				webDirs = append(webDirs, wd)
			}
			if err := wdRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query web probes for this host
		var webProbes []models.WebProbe
		wpRows, wpErr := db.Query(`
			SELECT id, port, COALESCE(scheme, ''), COALESCE(url, ''), COALESCE(title, ''),
				   COALESCE(status_code, 0), COALESCE(webserver, ''), COALESCE(content_type, ''),
				   COALESCE(content_length, 0), COALESCE(tech, ''), COALESCE(location, ''),
				   COALESCE(response_time, ''), COALESCE(source, 'manual')
			FROM web_probes
			WHERE host_id = ? AND project_id = ?
			ORDER BY port ASC, scheme ASC
		`, hostID, projectID)
		if wpErr == nil {
			defer wpRows.Close()
			for wpRows.Next() {
				var wp models.WebProbe
				wpRows.Scan(&wp.ID, &wp.Port, &wp.Scheme, &wp.URL, &wp.Title,
					&wp.StatusCode, &wp.WebServer, &wp.ContentType,
					&wp.ContentLength, &wp.Tech, &wp.Location,
					&wp.ResponseTime, &wp.Source)
				webProbes = append(webProbes, wp)
			}
			if err := wpRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query findings for this host (Issues tab)
		var hostFindings []models.HostFinding
		hfRows, hfErr := db.Query(`
			SELECT fh.id, f.id, f.title, f.severity, COALESCE(f.cvss_score, 0), fh.port, COALESCE(fh.protocol, ''), COALESCE(f.source, 'manual')
			FROM finding_hosts fh
			JOIN findings f ON fh.finding_id = f.id
			WHERE fh.host_id = ? AND f.project_id = ?
			ORDER BY f.cvss_score DESC, f.title ASC
		`, hostID, projectID)
		if hfErr == nil {
			defer hfRows.Close()
			for hfRows.Next() {
				var hf models.HostFinding
				if err := hfRows.Scan(&hf.FindingHostID, &hf.FindingID, &hf.Title, &hf.Severity, &hf.CvssScore, &hf.Port, &hf.Protocol, &hf.Source); err != nil {
					continue
				}
				hostFindings = append(hostFindings, hf)
			}
			if err := hfRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Find previous and next host IDs (by numeric IP order)
		var prevHostID, nextHostID int
		type hostNav struct {
			ID int
			IP string
		}
		navRows, navErr := db.Query("SELECT id, ip_address FROM hosts WHERE project_id = ?", projectID)
		if navErr == nil {
			var allHosts []hostNav
			for navRows.Next() {
				var hn hostNav
				navRows.Scan(&hn.ID, &hn.IP)
				allHosts = append(allHosts, hn)
			}
			if err := navRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
			navRows.Close()
			sort.Slice(allHosts, func(i, j int) bool {
				return ipToSortKey(allHosts[i].IP) < ipToSortKey(allHosts[j].IP)
			})
			for idx, hn := range allHosts {
				if hn.ID == h.ID {
					if idx > 0 {
						prevHostID = allHosts[idx-1].ID
					}
					if idx < len(allHosts)-1 {
						nextHostID = allHosts[idx+1].ID
					}
					break
				}
			}
		}

		c.HTML(http.StatusOK, "host_detail.html", gin.H{
			"username":       username,
			"project":        project,
			"page_type":      "hosts",
			"is_admin":       isAdmin,
			"host":           h,
			"services":       services,
			"hostnames":      hostnames,
			"host_creds":     hostCreds,
			"web_dirs":       webDirs,
			"web_probes":     webProbes,
			"host_findings":  hostFindings,
			"prev_host_id":   prevHostID,
			"next_host_id":   nextHostID,
			"ngrok_active":   IsNgrokActive(),
		})
	}
}

func UpdateHostInfo(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		os := c.PostForm("os")
		tag := c.PostForm("tag")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		_, err := db.Exec(
			"UPDATE hosts SET os = ?, tag = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?",
			os, tag, userID, hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update host"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func AddHostService(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		portStr := strings.TrimSpace(c.PostForm("port"))
		protocol := strings.TrimSpace(c.PostForm("protocol"))
		serviceName := strings.TrimSpace(c.PostForm("service_name"))

		if portStr == "" || protocol == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Port and protocol are required"})
			return
		}

		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid port number (1-65535)"})
			return
		}

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM services WHERE host_id = ? AND project_id = ? AND port = ? AND protocol = ?)",
			hostID, projectID, port, protocol).Scan(&exists)
		if exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Service with this port and protocol already exists on this host"})
			return
		}

		_, err = db.Exec("INSERT INTO services (host_id, project_id, port, protocol, service_name, source) VALUES (?, ?, ?, ?, ?, 'manual')",
			hostID, projectID, port, protocol, serviceName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add service"})
			return
		}

		autoColorNewHosts(db, projectID)
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func DeleteHostService(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		serviceID := c.PostForm("service_id")

		_, err := db.Exec("DELETE FROM services WHERE id = ? AND host_id = ? AND project_id = ?",
			serviceID, hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete service"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkDeleteHostServices(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM services WHERE id = ? AND host_id = ? AND project_id = ?",
				id, hostID, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

func UpdateServiceColor(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if hostID != "" && !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		serviceID := c.PostForm("service_id")
		color := c.PostForm("color")

		validColors := map[string]bool{
			"grey": true, "green": true, "blue": true,
			"yellow": true, "orange": true, "red": true,
		}
		if !validColors[color] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid color"})
			return
		}

		_, err := db.Exec("UPDATE services SET color = ? WHERE id = ? AND project_id = ?",
			color, serviceID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update color"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func AddHostHostname(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		hostname := strings.TrimSpace(c.PostForm("hostname"))

		if hostname == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Hostname is required"})
			return
		}

		_, err := db.Exec("INSERT INTO hostnames (host_id, project_id, hostname, source) VALUES (?, ?, ?, 'manual')",
			hostID, projectID, hostname)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Hostname already exists on this host"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func DeleteHostHostname(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		hostnameID := c.PostForm("hostname_id")

		_, err := db.Exec("DELETE FROM hostnames WHERE id = ? AND host_id = ? AND project_id = ?",
			hostnameID, hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete hostname"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkDeleteHostHostnames(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM hostnames WHERE id = ? AND host_id = ? AND project_id = ?",
				id, hostID, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

func SetDefaultHostname(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		hostnameID := c.PostForm("hostname_id")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Get the hostname text, validating it belongs to this host+project
		var hostnameText string
		err := db.QueryRow("SELECT hostname FROM hostnames WHERE id = ? AND host_id = ? AND project_id = ?",
			hostnameID, hostID, projectID).Scan(&hostnameText)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Hostname not found"})
			return
		}

		// Update hosts.hostname to the selected hostname
		_, err = db.Exec("UPDATE hosts SET hostname = ?, last_modified_by = ?, updated_at = datetime('now') WHERE id = ? AND project_id = ?",
			hostnameText, userID, hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set default hostname"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "hostname": hostnameText})
	}
}

func ProjectServices(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		// Get aggregated services - group by all 4 columns for true deduplication
		rows, err := db.Query(`
			SELECT
				s.port,
				s.protocol,
				s.service_name,
				s.version,
				GROUP_CONCAT(DISTINCT h.ip_address) as hosts,
				COUNT(DISTINCT s.host_id) as host_count
			FROM services s
			INNER JOIN hosts h ON s.host_id = h.id
			WHERE s.project_id = ?
			GROUP BY s.port, s.protocol, s.service_name, s.version
			ORDER BY s.port ASC
		`, projectID)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": "Failed to load services"})
			return
		}
		defer rows.Close()

		var services []models.AggregatedService
		for rows.Next() {
			var svc models.AggregatedService
			var protocol, serviceName, banner, hostIPs sql.NullString
			rows.Scan(&svc.Port, &protocol, &serviceName, &banner, &hostIPs, &svc.HostCount)
			svc.Protocol = protocol.String
			svc.ServiceName = serviceName.String
			svc.Banner = banner.String
			svc.HostIPs = hostIPs.String
			services = append(services, svc)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		c.HTML(http.StatusOK, "services.html", gin.H{
			"username":     username,
			"project":      project,
			"page_type":    "services",
			"is_admin":     isAdmin,
			"services":     services,
			"ngrok_active": IsNgrokActive(),
		})
	}
}

func ProjectFindings(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		rows, err := db.Query(`
			SELECT f.id, f.project_id, f.title, f.severity,
				   COALESCE(f.cvss_score, 0), COALESCE(f.cvss_vector, ''),
				   COALESCE(f.description, ''), COALESCE(f.synopsis, ''),
				   COALESCE(f.solution, ''), COALESCE(f.evidence, ''),
				   COALESCE(f.plugin_id, ''), COALESCE(f.plugin_source, ''),
				   COALESCE(f.color, 'grey'), COALESCE(f.source, 'manual'),
				   COALESCE(u.username, '') as modified_by,
				   (SELECT COUNT(*) FROM finding_hosts fh WHERE fh.finding_id = f.id) as host_count,
				   f.created_at, f.updated_at
			FROM findings f
			LEFT JOIN users u ON f.last_modified_by = u.id
			WHERE f.project_id = ?
			ORDER BY f.cvss_score DESC, f.title ASC
		`, projectID)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": "Failed to load findings"})
			return
		}
		defer rows.Close()

		var findings []models.FindingWithUser
		for rows.Next() {
			var f models.FindingWithUser
			rows.Scan(&f.ID, &f.ProjectID, &f.Title, &f.Severity,
				&f.CvssScore, &f.CvssVector,
				&f.Description, &f.Synopsis,
				&f.Solution, &f.Evidence,
				&f.PluginID, &f.PluginSource,
				&f.Color, &f.Source,
				&f.ModifiedByUsername, &f.HostCount,
				&f.CreatedAt, &f.UpdatedAt)
			findings = append(findings, f)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		c.HTML(http.StatusOK, "findings.html", gin.H{
			"username":     username,
			"project":      project,
			"page_type":    "findings",
			"is_admin":     isAdmin,
			"findings":     findings,
			"ngrok_active": IsNgrokActive(),
		})
	}
}

func ProjectCredentials(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		rows, err := db.Query("SELECT id, username, password, credential_type, host, service, notes, created_at FROM credentials WHERE project_id = ? ORDER BY username ASC", projectID)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": "Failed to load credentials"})
			return
		}
		defer rows.Close()

		var creds []models.Credential
		for rows.Next() {
			var cr models.Credential
			rows.Scan(&cr.ID, &cr.Username, &cr.Password, &cr.CredentialType, &cr.Host, &cr.Service, &cr.Notes, &cr.CreatedAt)
			cr.ProjectID = projectID
			creds = append(creds, cr)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		c.HTML(http.StatusOK, "credentials.html", gin.H{
			"username":     username,
			"project":      project,
			"page_type":    "credentials",
			"is_admin":     isAdmin,
			"credentials":  creds,
			"ngrok_active": IsNgrokActive(),
		})
	}
}

func AddCredential(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")

		username := strings.TrimSpace(c.PostForm("username"))
		password := strings.TrimSpace(c.PostForm("password"))
		credType := strings.TrimSpace(c.PostForm("credential_type"))
		host := strings.TrimSpace(c.PostForm("host"))
		service := strings.TrimSpace(c.PostForm("service"))
		notes := strings.TrimSpace(c.PostForm("notes"))

		if username == "" && password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username or password is required"})
			return
		}

		_, err := db.Exec("INSERT INTO credentials (project_id, username, password, credential_type, host, service, notes) VALUES (?, ?, ?, ?, ?, ?, ?)",
			projectID, username, password, credType, host, service, notes)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add credential"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func DeleteCredential(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		credID := c.PostForm("cred_id")

		_, err := db.Exec("DELETE FROM credentials WHERE id = ? AND project_id = ?", credID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete credential"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkDeleteCredentials(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM credentials WHERE id = ? AND project_id = ?", id, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

func ExportCredentials(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")

		rows, err := db.Query("SELECT username, password, host, service, credential_type, notes FROM credentials WHERE project_id = ? ORDER BY username ASC", projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export credentials"})
			return
		}
		defer rows.Close()

		var buf strings.Builder
		buf.WriteString("Username,Password,Host,Service,Type,Notes\n")
		for rows.Next() {
			var u, p, h, s, t, n string
			rows.Scan(&u, &p, &h, &s, &t, &n)
			buf.WriteString(csvEscape(u) + "," + csvEscape(p) + "," + csvEscape(h) + "," + csvEscape(s) + "," + csvEscape(t) + "," + csvEscape(n) + "\n")
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		c.Header("Content-Disposition", "attachment; filename=credentials.csv")
		c.Data(http.StatusOK, "text/csv", []byte(buf.String()))
	}
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

func ProjectUsers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		rows, err := db.Query("SELECT id, username, created_at FROM discovered_users WHERE project_id = ? ORDER BY username ASC", projectID)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": "Failed to load users"})
			return
		}
		defer rows.Close()

		var discoveredUsers []models.DiscoveredUser
		for rows.Next() {
			var u models.DiscoveredUser
			rows.Scan(&u.ID, &u.Username, &u.CreatedAt)
			u.ProjectID = projectID
			discoveredUsers = append(discoveredUsers, u)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		c.HTML(http.StatusOK, "users.html", gin.H{
			"username":         username,
			"project":          project,
			"page_type":        "users",
			"is_admin":         isAdmin,
			"discovered_users": discoveredUsers,
			"ngrok_active":     IsNgrokActive(),
		})
	}
}

func AddDiscoveredUser(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		name := strings.TrimSpace(c.PostForm("username"))

		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username is required"})
			return
		}

		_, err := db.Exec("INSERT INTO discovered_users (project_id, username) VALUES (?, ?)", projectID, name)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Username already exists in this project"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkAddDiscoveredUsers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		usernames := c.PostForm("usernames")

		added, skipped := addDiscoveredUsers(db, projectID, usernames)

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"added":   added,
			"skipped": skipped,
		})
	}
}

func UploadDiscoveredUsers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")

		file, _, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No file provided"})
			return
		}
		defer file.Close()

		content, err := io.ReadAll(file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
			return
		}

		added, skipped := addDiscoveredUsers(db, projectID, string(content))

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"added":   added,
			"skipped": skipped,
		})
	}
}

func DeleteDiscoveredUser(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		userIDStr := c.PostForm("user_id")

		_, err := db.Exec("DELETE FROM discovered_users WHERE id = ? AND project_id = ?", userIDStr, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete user"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func ExportDiscoveredUsers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")

		rows, err := db.Query("SELECT username FROM discovered_users WHERE project_id = ? ORDER BY username ASC", projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export users"})
			return
		}
		defer rows.Close()

		var lines []string
		for rows.Next() {
			var name string
			rows.Scan(&name)
			lines = append(lines, name)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		content := strings.Join(lines, "\n") + "\n"
		c.Header("Content-Disposition", "attachment; filename=discovered_users.txt")
		c.Data(http.StatusOK, "text/plain", []byte(content))
	}
}

func BulkDeleteDiscoveredUsers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM discovered_users WHERE id = ? AND project_id = ?", id, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

func addDiscoveredUsers(db *sql.DB, projectID string, usernames string) (added int, skipped int) {
	lines := strings.Split(usernames, "\n")
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}

		_, err := db.Exec("INSERT INTO discovered_users (project_id, username) VALUES (?, ?)", projectID, name)
		if err != nil {
			skipped++
			continue
		}
		added++
	}
	return
}

func ProjectUploads(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		// Get uploads for this project
		rows, err := db.Query(`
			SELECT u.id, u.project_id, u.filename, u.stored_path, u.file_size, u.tool_type, u.uploaded_by, u.created_at, usr.username
			FROM uploads u
			INNER JOIN users usr ON u.uploaded_by = usr.id
			WHERE u.project_id = ?
			ORDER BY u.created_at DESC
		`, projectID)

		var uploads []models.UploadWithUser
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var u models.UploadWithUser
				rows.Scan(&u.ID, &u.ProjectID, &u.Filename, &u.StoredPath, &u.FileSize, &u.ToolType, &u.UploadedBy, &u.CreatedAt, &u.Username)
				uploads = append(uploads, u)
			}
			if err := rows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		c.HTML(http.StatusOK, "uploads.html", gin.H{
			"username":     username,
			"project":      project,
			"page_type":    "uploads",
			"is_admin":     isAdmin,
			"uploads":      uploads,
			"ngrok_active": IsNgrokActive(),
		})
	}
}

// detectToolType reads file content and determines the scan tool type
func detectToolType(filePath string, filename string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// Read first 4096 bytes for detection
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	content := string(buf[:n])
	contentLower := strings.ToLower(content)
	ext := strings.ToLower(filepath.Ext(filename))

	// 0. Atlas Raw: JSON with _atlas_export marker
	if strings.Contains(content, "\"_atlas_export\"") {
		return "atlas_raw", nil
	}

	// 0b. LAIR: JSON with industry + owner + contributors (unique LAIR top-level fields, appear early in file)
	if strings.Contains(content, "\"industry\"") && strings.Contains(content, "\"owner\"") && strings.Contains(content, "\"contributors\"") {
		return "lair", nil
	}

	// 1. Nessus: .nessus extension or XML containing NessusClientData
	if ext == ".nessus" || strings.Contains(contentLower, "<nessusclientdata") {
		return "nessus", nil
	}

	// 2. Nmap: XML containing <nmaprun (XML only)
	if strings.Contains(contentLower, "<nmaprun") {
		return "nmap", nil
	}

	// 3. BBOT: JSON/JSONL with scope_distance (unique to BBOT, checked before nuclei/httpx)
	if strings.Contains(content, "scope_distance") {
		return "bbot", nil
	}

	// 4. HTTPX: JSON/JSONL with status_code + webserver/content_type (unique combo)
	if strings.Contains(content, "status_code") && (strings.Contains(content, "\"webserver\"") || strings.Contains(content, "\"content_type\"")) {
		return "httpx", nil
	}

	// 5. Nuclei: JSON/JSONL with template-id (only if not BBOT or HTTPX)
	if strings.Contains(content, "template-id") {
		return "nuclei", nil
	}

	return "", fmt.Errorf("Unrecognized file format. Supported formats:\n• Nmap XML (.xml)\n• Nessus (.nessus)\n• Nuclei (.json, .jsonl)\n• BBOT (.json, .jsonl)\n• HTTPX (.json, .jsonl)\n• Atlas Raw (.json)")
}

// Nmap XML parsing structs
type NmapRun struct {
	XMLName xml.Name   `xml:"nmaprun"`
	Hosts   []NmapHost `xml:"host"`
}

type NmapHost struct {
	Addresses []NmapAddress  `xml:"address"`
	Hostnames []NmapHostname `xml:"hostnames>hostname"`
	Ports     []NmapPort     `xml:"ports>port"`
	OS        []NmapOS       `xml:"os>osmatch"`
}

type NmapAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
}

type NmapHostname struct {
	Name string `xml:"name,attr"`
}

type NmapPort struct {
	Protocol string      `xml:"protocol,attr"`
	PortID   int         `xml:"portid,attr"`
	State    NmapState   `xml:"state"`
	Service  NmapService `xml:"service"`
}

type NmapState struct {
	State string `xml:"state,attr"`
}

type NmapService struct {
	Name    string `xml:"name,attr"`
	Product string `xml:"product,attr"`
	Version string `xml:"version,attr"`
}

type NmapOS struct {
	Name string `xml:"name,attr"`
}

// Nessus XML parsing structs
type NessusClientDataV2 struct {
	XMLName xml.Name       `xml:"NessusClientData_v2"`
	Report  NessusReport   `xml:"Report"`
}

type NessusReport struct {
	ReportHosts []NessusReportHost `xml:"ReportHost"`
}

type NessusReportHost struct {
	Name           string                `xml:"name,attr"`
	HostProperties NessusHostProperties  `xml:"HostProperties"`
	ReportItems    []NessusReportItem    `xml:"ReportItem"`
}

type NessusHostProperties struct {
	Tags []NessusTag `xml:"tag"`
}

type NessusTag struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

type NessusReportItem struct {
	Port           int    `xml:"port,attr"`
	Protocol       string `xml:"protocol,attr"`
	ServiceName    string `xml:"svc_name,attr"`
	PluginName     string `xml:"pluginName,attr"`
	PluginID       string `xml:"pluginID,attr"`
	PluginFamily   string `xml:"pluginFamily,attr"`
	Severity       int    `xml:"severity,attr"`
	Description    string `xml:"description"`
	Solution       string `xml:"solution"`
	Synopsis       string `xml:"synopsis"`
	PluginOutput   string `xml:"plugin_output"`
	RiskFactor     string `xml:"risk_factor"`
	Cvss3BaseScore string `xml:"cvss3_base_score"`
	Cvss3Vector    string `xml:"cvss3_vector"`
	CvssBaseScore  string `xml:"cvss_base_score"`
	CVEs           []string `xml:"cve"`
}

// serviceNameAliases maps common service name variations to canonical names
var serviceNameAliases = map[string]string{
	"www":        "http",
	"www-http":   "http",
	"http-proxy": "http",
	"https":      "https",
	"ssl/http":   "https",
	"ssl/https":  "https",
	"domain":     "dns",
	"sunrpc":     "rpcbind",
	"netbios-ns": "netbios",
	"netbios-ssn": "netbios",
	"netbios-dgm": "netbios",
	"microsoft-ds": "smb",
	"ms-wbt-server": "rdp",
	"ms-sql-s":   "mssql",
	"mysql":      "mysql",
	"postgresql": "postgres",
	"imaps":      "imap",
	"pop3s":      "pop3",
	"smtps":      "smtp",
	"submission": "smtp",
	"ntp":        "ntp",
	"snmp":       "snmp",
	"ldap":       "ldap",
	"ldaps":      "ldap",
	"kerberos":   "kerberos",
	"kerberos-sec": "kerberos",
}

// normalizeServiceName converts service name aliases to canonical names (uppercase)
func normalizeServiceName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if canonical, ok := serviceNameAliases[lower]; ok {
		return strings.ToUpper(canonical)
	}
	return strings.ToUpper(lower)
}

// parseNmapFile extracts hosts and services from an nmap XML file
func parseNmapFile(filePath string, projectID string, db *sql.DB, userID int) (hostsAdded, servicesAdded, skipped int, err error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, 0, 0, err
	}

	var nmapRun NmapRun
	if err := xml.Unmarshal(data, &nmapRun); err != nil {
		return 0, 0, 0, err
	}

	for _, host := range nmapRun.Hosts {
		// Get IP address (prefer ipv4) and MAC address
		var ipAddr, macAddr string
		for _, addr := range host.Addresses {
			switch addr.AddrType {
			case "ipv4":
				ipAddr = addr.Addr
			case "mac":
				macAddr = addr.Addr
			}
		}
		if ipAddr == "" {
			continue
		}

		// Get hostname
		var hostname string
		if len(host.Hostnames) > 0 {
			hostname = host.Hostnames[0].Name
		}

		// Get OS
		var osName string
		if len(host.OS) > 0 {
			osName = host.OS[0].Name
		}

		// Insert or get existing host
		var hostID int64
		err := db.QueryRow("SELECT id FROM hosts WHERE project_id = ? AND ip_address = ?",
			projectID, ipAddr).Scan(&hostID)
		if err == sql.ErrNoRows {
			result, err := db.Exec("INSERT INTO hosts (project_id, ip_address, hostname, os, mac_address, color, source, last_modified_by) VALUES (?, ?, ?, ?, ?, 'yellow', 'nmap', ?)",
				projectID, ipAddr, hostname, osName, macAddr, userID)
			if err != nil {
				continue
			}
			hostID, _ = result.LastInsertId()
			hostsAdded++
		} else if err != nil {
			continue
		} else {
			// Host already exists - fill in OS and MAC if currently empty
			db.Exec(`UPDATE hosts SET
				os = CASE WHEN COALESCE(os, '') = '' AND ? != '' THEN ? ELSE os END,
				mac_address = CASE WHEN COALESCE(mac_address, '') = '' AND ? != '' THEN ? ELSE mac_address END,
				last_modified_by = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`, osName, osName, macAddr, macAddr, userID, hostID)
		}

		// Insert all hostnames into hostnames table
		for _, hn := range host.Hostnames {
			name := strings.TrimSpace(hn.Name)
			if name != "" {
				db.Exec("INSERT OR IGNORE INTO hostnames (host_id, project_id, hostname, source) VALUES (?, ?, ?, 'nmap')",
					hostID, projectID, name)
			}
		}

		// Insert services (only open or tcpwrapped)
		for _, port := range host.Ports {
			state := strings.ToLower(port.State.State)
			if state != "open" && state != "tcpwrapped" {
				continue
			}

			// Build banner from product + version, normalize service name and protocol to uppercase
			banner := strings.TrimSpace(port.Service.Product + " " + port.Service.Version)
			serviceName := normalizeServiceName(port.Service.Name)
			protocol := strings.ToUpper(port.Protocol)

			// Check for exact duplicate (all 4 columns match on same host)
			var exists bool
			db.QueryRow(`SELECT EXISTS(SELECT 1 FROM services
				WHERE host_id = ? AND port = ? AND protocol = ?
				AND (service_name = ? OR (service_name IS NULL AND ? = ''))
				AND (version = ? OR (version IS NULL AND ? = '')))`,
				hostID, port.PortID, protocol,
				serviceName, serviceName, banner, banner).Scan(&exists)

			if !exists {
				_, err := db.Exec(`INSERT INTO services (host_id, project_id, port, protocol, service_name, version, source)
					VALUES (?, ?, ?, ?, ?, ?, 'nmap')`,
					hostID, projectID, port.PortID, protocol, serviceName, banner)
				if err == nil {
					servicesAdded++
				}
			} else {
				skipped++
			}
		}
	}

	return hostsAdded, servicesAdded, skipped, nil
}

// parseNessusFile extracts hosts, services, and findings from a Nessus XML file
func parseNessusFile(filePath string, projectID string, db *sql.DB, userID int) (hostsAdded, servicesAdded, findingsAdded, skipped int, err error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, 0, 0, 0, err
	}

	var nessus NessusClientDataV2
	if err := xml.Unmarshal(data, &nessus); err != nil {
		return 0, 0, 0, 0, err
	}

	// Cache for finding dedup: pluginID -> DB finding ID
	findingCache := make(map[string]int64)

	for _, host := range nessus.Report.ReportHosts {
		// Get IP address and other properties from host properties
		var ipAddr, hostname, osName, macAddr string
		var nessusHostnames []string
		ipAddr = host.Name // Default to the name attribute which is usually the IP

		for _, tag := range host.HostProperties.Tags {
			switch tag.Name {
			case "host-ip":
				ipAddr = tag.Value
			case "host-fqdn":
				hostname = tag.Value
				if strings.TrimSpace(tag.Value) != "" {
					nessusHostnames = append(nessusHostnames, strings.TrimSpace(tag.Value))
				}
			case "host-rdns":
				if strings.TrimSpace(tag.Value) != "" {
					nessusHostnames = append(nessusHostnames, strings.TrimSpace(tag.Value))
				}
			case "operating-system":
				osName = tag.Value
			case "mac-address":
				macAddr = strings.TrimSpace(tag.Value)
			}
		}

		if ipAddr == "" {
			continue
		}

		// Insert or get existing host
		var hostID int64
		err := db.QueryRow("SELECT id FROM hosts WHERE project_id = ? AND ip_address = ?",
			projectID, ipAddr).Scan(&hostID)
		if err == sql.ErrNoRows {
			result, err := db.Exec("INSERT INTO hosts (project_id, ip_address, hostname, os, mac_address, color, source, last_modified_by) VALUES (?, ?, ?, ?, ?, 'yellow', 'nessus', ?)",
				projectID, ipAddr, hostname, osName, macAddr, userID)
			if err != nil {
				continue
			}
			hostID, _ = result.LastInsertId()
			hostsAdded++
		} else if err != nil {
			continue
		} else {
			// Host already exists - fill in OS and MAC if currently empty
			db.Exec(`UPDATE hosts SET
				os = CASE WHEN COALESCE(os, '') = '' AND ? != '' THEN ? ELSE os END,
				mac_address = CASE WHEN COALESCE(mac_address, '') = '' AND ? != '' THEN ? ELSE mac_address END,
				last_modified_by = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`, osName, osName, macAddr, macAddr, userID, hostID)
		}

		// Insert hostnames into hostnames table
		for _, hn := range nessusHostnames {
			db.Exec("INSERT OR IGNORE INTO hostnames (host_id, project_id, hostname, source) VALUES (?, ?, ?, 'nessus')",
				hostID, projectID, hn)
		}

		// Track unique services per host (port+protocol+serviceName combo)
		seenServices := make(map[string]bool)

		// Insert services from report items (skip port 0 which is host-level info)
		for _, item := range host.ReportItems {
			if item.Port == 0 {
				continue
			}

			// Normalize service name and protocol to uppercase
			serviceName := normalizeServiceName(item.ServiceName)
			protocol := strings.ToUpper(item.Protocol)

			// Create unique key for this service on this host
			serviceKey := fmt.Sprintf("%d-%s-%s", item.Port, protocol, serviceName)
			if seenServices[serviceKey] {
				continue
			}
			seenServices[serviceKey] = true

			// Check for exact duplicate (all 4 columns match on same host)
			var exists bool
			db.QueryRow(`SELECT EXISTS(SELECT 1 FROM services
				WHERE host_id = ? AND port = ? AND protocol = ?
				AND (service_name = ? OR (service_name IS NULL AND ? = ''))
				AND (version = '' OR version IS NULL))`,
				hostID, item.Port, protocol,
				serviceName, serviceName).Scan(&exists)

			if !exists {
				_, err := db.Exec(`INSERT INTO services (host_id, project_id, port, protocol, service_name, version, source)
					VALUES (?, ?, ?, ?, ?, ?, 'nessus')`,
					hostID, projectID, item.Port, protocol, serviceName, "")
				if err == nil {
					servicesAdded++
				}
			} else {
				skipped++
			}
		}

		// Findings pass: extract findings from report items with severity > 0
		for _, item := range host.ReportItems {
			if item.Severity == 0 {
				continue
			}

			// Parse CVSS score (prefer v3, fallback v2)
			var cvssScore float64
			var cvssVector string
			if item.Cvss3BaseScore != "" {
				fmt.Sscanf(item.Cvss3BaseScore, "%f", &cvssScore)
				cvssVector = item.Cvss3Vector
			} else if item.CvssBaseScore != "" {
				fmt.Sscanf(item.CvssBaseScore, "%f", &cvssScore)
			}

			// Derive severity from CVSS score
			severity := cvssToSeverity(cvssScore)

			pluginID := item.PluginID

			// Check findingCache or query DB for existing finding with same project+plugin_id
			var findingID int64
			if cachedID, ok := findingCache[pluginID]; ok {
				findingID = cachedID
			} else {
				err := db.QueryRow("SELECT id FROM findings WHERE project_id = ? AND plugin_id = ?",
					projectID, pluginID).Scan(&findingID)
				if err == nil {
					findingCache[pluginID] = findingID
				}
			}

			// If new finding, insert it
			if findingID == 0 {
				result, insErr := db.Exec(`INSERT INTO findings (project_id, title, severity, cvss_score, cvss_vector,
					description, synopsis, solution, evidence, plugin_id, plugin_source, source, last_modified_by)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'nessus', ?)`,
					projectID, item.PluginName, severity, cvssScore, cvssVector,
					item.Description, item.Synopsis, item.Solution, item.PluginOutput,
					pluginID, item.PluginFamily, userID)
				if insErr != nil {
					continue
				}
				findingID, _ = result.LastInsertId()
				findingCache[pluginID] = findingID
				findingsAdded++
			}

			// Link finding to host
			protocol := strings.ToUpper(item.Protocol)
			db.Exec(`INSERT OR IGNORE INTO finding_hosts (finding_id, host_id, port, protocol, plugin_output)
				VALUES (?, ?, ?, ?, ?)`,
				findingID, hostID, item.Port, protocol, item.PluginOutput)

			// Insert CVEs
			for _, cve := range item.CVEs {
				cve = strings.TrimSpace(cve)
				if cve != "" {
					db.Exec("INSERT OR IGNORE INTO finding_cves (finding_id, cve) VALUES (?, ?)",
						findingID, cve)
				}
			}
		}
	}

	return hostsAdded, servicesAdded, findingsAdded, skipped, nil
}

// parseBbotFile adds hostnames and web directories from a BBOT JSON file to existing hosts only
func parseBbotFile(filePath string, projectID string, db *sql.DB) (hostsAdded, servicesAdded, skipped int, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()

	// First pass: collect IP_ADDRESS events to build host map (IP -> hostID)
	// Also collect DNS_NAME and URL events for second pass
	type bbotEvent struct {
		Type          string          `json:"type"`
		Data          json.RawMessage `json:"data"`
		Host          string          `json:"host"`
		Port          int             `json:"port"`
		ResolvedHosts []string        `json:"resolved_hosts"`
		Tags          []string        `json:"tags"`
	}

	// hostID cache: IP -> database host ID
	hostIDs := make(map[string]int64)

	// getExistingHost looks up an existing host by IP — returns 0 if not found (never creates)
	getExistingHost := func(ipAddr string) int64 {
		if id, ok := hostIDs[ipAddr]; ok {
			return id
		}
		var hostID int64
		err := db.QueryRow("SELECT id FROM hosts WHERE project_id = ? AND ip_address = ?",
			projectID, ipAddr).Scan(&hostID)
		if err != nil {
			return 0
		}
		hostIDs[ipAddr] = hostID
		return hostID
	}

	// Collect all events by type
	var dnsEvents []bbotEvent
	var urlEvents []bbotEvent

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0), 10*1024*1024) // 10MB max line size
	for scanner.Scan() {
		line := scanner.Bytes()
		var event bbotEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		switch event.Type {
		case "IP_ADDRESS":
			var ipData string
			if err := json.Unmarshal(event.Data, &ipData); err != nil {
				continue
			}
			ipAddr := strings.TrimSpace(ipData)
			if ipAddr == "" {
				continue
			}
			hostID := getExistingHost(ipAddr)
			if hostID == 0 {
				skipped++
				continue
			}

			// Extract PTR hostnames from dns_children
			var fullEvent struct {
				DNSChildren map[string][]string `json:"dns_children"`
			}
			json.Unmarshal(line, &fullEvent)
			if ptrs, ok := fullEvent.DNSChildren["PTR"]; ok {
				for _, ptr := range ptrs {
					ptr = strings.TrimSpace(strings.TrimSuffix(ptr, "."))
					if ptr != "" {
						db.Exec("INSERT OR IGNORE INTO hostnames (host_id, project_id, hostname, source) VALUES (?, ?, ?, 'bbot')",
							hostID, projectID, ptr)
					}
				}
			}

		case "DNS_NAME":
			dnsEvents = append(dnsEvents, event)
		case "URL":
			urlEvents = append(urlEvents, event)
		}
	}

	// Second pass: process DNS_NAME events — link hostnames to hosts via resolved_hosts
	for _, event := range dnsEvents {
		var hostname string
		if err := json.Unmarshal(event.Data, &hostname); err != nil {
			continue
		}
		hostname = strings.TrimSpace(strings.TrimSuffix(hostname, "."))
		if hostname == "" {
			continue
		}

		// Link to each resolved host IP (only if host already exists)
		for _, ip := range event.ResolvedHosts {
			ip = strings.TrimSpace(ip)
			if ip == "" {
				continue
			}
			hostID := getExistingHost(ip)
			if hostID > 0 {
				db.Exec("INSERT OR IGNORE INTO hostnames (host_id, project_id, hostname, source) VALUES (?, ?, ?, 'bbot')",
					hostID, projectID, hostname)
			}
		}
	}

	// Third pass: process URL events — extract web directories
	for _, event := range urlEvents {
		var rawURL string
		if err := json.Unmarshal(event.Data, &rawURL); err != nil {
			continue
		}
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}

		parsed, err := url.Parse(rawURL)
		if err != nil {
			continue
		}

		// Determine host IP from the event's host field or resolved_hosts
		hostIP := strings.TrimSpace(event.Host)
		if hostIP == "" {
			continue
		}

		// Determine port
		port := event.Port
		if port == 0 {
			if parsed.Scheme == "https" {
				port = 443
			} else {
				port = 80
			}
		}

		// Extract path
		path := parsed.Path
		if path == "" {
			path = "/"
		}

		// Determine base_domain: use the hostname from the URL
		// This could be an IP or a domain name
		baseDomain := parsed.Hostname()

		// Find the host in DB — only if it already exists
		hostID := getExistingHost(hostIP)
		if hostID <= 0 {
			skipped++
			continue
		}

		_, insertErr := db.Exec("INSERT OR IGNORE INTO web_directories (host_id, project_id, port, base_domain, path, source) VALUES (?, ?, ?, ?, ?, 'bbot')",
			hostID, projectID, port, baseDomain, path)
		if insertErr != nil {
			skipped++
		}
	}

	return hostsAdded, 0, skipped, nil
}

// parseHttpxFile extracts web probe data from an HTTPX JSONL file and adds to existing hosts only
func parseHttpxFile(filePath string, projectID string, db *sql.DB) (hostsAdded, servicesAdded, skipped int, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, 0, 0, err
	}
	defer f.Close()

	type httpxEvent struct {
		Host          string   `json:"host"`
		Port          string   `json:"port"`
		Scheme        string   `json:"scheme"`
		URL           string   `json:"url"`
		Title         string   `json:"title"`
		StatusCode    int      `json:"status_code"`
		WebServer     string   `json:"webserver"`
		ContentType   string   `json:"content_type"`
		ContentLength int      `json:"content_length"`
		Tech          []string `json:"tech"`
		Location      string   `json:"location"`
		Time          string   `json:"time"`
	}

	// hostID cache: IP -> database host ID
	hostIDs := make(map[string]int64)

	// Also build hostname->IP map from hostnames table for this project
	hostnameToIPs := make(map[string][]string)
	hnRows, hnErr := db.Query(`
		SELECT h.ip_address, hn.hostname
		FROM hostnames hn
		JOIN hosts h ON hn.host_id = h.id
		WHERE hn.project_id = ?
	`, projectID)
	if hnErr == nil {
		defer hnRows.Close()
		for hnRows.Next() {
			var ip, hostname string
			hnRows.Scan(&ip, &hostname)
			hostnameToIPs[strings.ToLower(hostname)] = append(hostnameToIPs[strings.ToLower(hostname)], ip)
		}
		if err := hnRows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}
	}

	getExistingHost := func(ipAddr string) int64 {
		if id, ok := hostIDs[ipAddr]; ok {
			return id
		}
		var hostID int64
		err := db.QueryRow("SELECT id FROM hosts WHERE project_id = ? AND ip_address = ?",
			projectID, ipAddr).Scan(&hostID)
		if err != nil {
			return 0
		}
		hostIDs[ipAddr] = hostID
		return hostID
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var event httpxEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}

		hostIP := strings.TrimSpace(event.Host)
		if hostIP == "" {
			skipped++
			continue
		}

		// Try to find host by IP first
		hostID := getExistingHost(hostIP)

		// If not found by IP, check if it's a hostname and resolve to an existing host
		if hostID == 0 {
			if ips, ok := hostnameToIPs[strings.ToLower(hostIP)]; ok {
				for _, ip := range ips {
					hostID = getExistingHost(ip)
					if hostID > 0 {
						break
					}
				}
			}
		}

		if hostID == 0 {
			skipped++
			continue
		}

		// Parse port
		port := 0
		if event.Port != "" {
			fmt.Sscanf(event.Port, "%d", &port)
		}
		if port == 0 {
			if event.Scheme == "https" {
				port = 443
			} else {
				port = 80
			}
		}

		// Join tech array
		techStr := strings.Join(event.Tech, ", ")

		// Insert or update web probe (ON CONFLICT update with new data)
		_, insertErr := db.Exec(`INSERT INTO web_probes (host_id, project_id, port, scheme, url, title, status_code, webserver, content_type, content_length, tech, location, response_time, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'httpx')
			ON CONFLICT(host_id, port, scheme) DO UPDATE SET
				url=excluded.url, title=excluded.title, status_code=excluded.status_code,
				webserver=excluded.webserver, content_type=excluded.content_type,
				content_length=excluded.content_length, tech=excluded.tech,
				location=excluded.location, response_time=excluded.response_time,
				source=excluded.source`,
			hostID, projectID, port, event.Scheme, event.URL, event.Title,
			event.StatusCode, event.WebServer, event.ContentType,
			event.ContentLength, techStr, event.Location, event.Time)
		if insertErr != nil {
			skipped++
		} else {
			servicesAdded++
		}
	}

	return 0, servicesAdded, skipped, nil
}

// parseNucleiFile parses a Nuclei JSONL output file.
// Nuclei is supplemental: it creates findings and maps them to existing hosts.
// It does NOT create new hosts — only hosts already in the project are matched.
func parseNucleiFile(filePath string, projectID string, db *sql.DB, userID int) (findingsAdded, hostsMatched, skipped int, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// Pre-load project hosts into map: ip -> host_id
	hostMap := make(map[string]int64)
	hRows, hErr := db.Query("SELECT id, ip_address FROM hosts WHERE project_id = ?", projectID)
	if hErr != nil {
		return 0, 0, 0, fmt.Errorf("failed to query hosts: %w", hErr)
	}
	defer hRows.Close()
	for hRows.Next() {
		var id int64
		var ip string
		hRows.Scan(&id, &ip)
		hostMap[ip] = id
	}
	if err := hRows.Err(); err != nil {
		log.Printf("Row iteration error: %v", err)
	}

	// Cache: template-id -> finding ID (to avoid duplicate lookups)
	findingCache := make(map[string]int64)

	type nucleiEntry struct {
		TemplateID string `json:"template-id"`
		IP         string `json:"ip"`
		Host       string `json:"host"`
		Port       string `json:"port"`
		Scheme     string `json:"scheme"`
		URL        string `json:"url"`
		MatchedAt  string `json:"matched-at"`
		Info       struct {
			Name           string   `json:"name"`
			Description    string   `json:"description"`
			Impact         string   `json:"impact"`
			Severity       string   `json:"severity"`
			Tags           []string `json:"tags"`
			Reference      []string `json:"reference"`
			Remediation    string   `json:"remediation"`
			Classification struct {
				CvssScore   interface{} `json:"cvss-score"`
				CvssMetrics string      `json:"cvss-metrics"`
				CveID       interface{} `json:"cve-id"`
				CweID       interface{} `json:"cwe-id"`
			} `json:"classification"`
		} `json:"info"`
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0), 10*1024*1024) // 10MB line buffer

	matchedHostSet := make(map[int64]bool)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry nucleiEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			skipped++
			continue
		}

		// Skip entries with no IP (e.g. PTR fingerprints)
		ip := strings.TrimSpace(entry.IP)
		if ip == "" {
			skipped++
			continue
		}

		// Must match an existing host in the project
		hostID, ok := hostMap[ip]
		if !ok {
			skipped++
			continue
		}

		// Parse CVSS score
		var cvssScore float64
		switch v := entry.Info.Classification.CvssScore.(type) {
		case float64:
			cvssScore = v
		case string:
			fmt.Sscanf(v, "%f", &cvssScore)
		}
		cvssVector := entry.Info.Classification.CvssMetrics

		// Derive severity from CVSS; fall back to nuclei's own severity string for info-level
		severity := cvssToSeverity(cvssScore)

		// Parse CVE IDs (can be []string or nil)
		var cveIDs []string
		switch v := entry.Info.Classification.CveID.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					cveIDs = append(cveIDs, strings.ToUpper(s))
				}
			}
		}

		// Build finding title from template name
		title := entry.Info.Name
		if title == "" {
			title = entry.TemplateID
		}

		// Description: combine description + impact
		description := strings.TrimSpace(entry.Info.Description)
		if entry.Info.Impact != "" {
			if description != "" {
				description += "\n\n"
			}
			description += strings.TrimSpace(entry.Info.Impact)
		}

		solution := strings.TrimSpace(entry.Info.Remediation)

		// Evidence: the matched URL
		evidence := ""
		if entry.MatchedAt != "" {
			evidence = "Matched at: " + entry.MatchedAt
		}

		// Look up or create the finding (deduplicate by template-id within project)
		var findingID int64
		if cachedID, ok := findingCache[entry.TemplateID]; ok {
			findingID = cachedID
		} else {
			// Try to find existing finding with same template-id as plugin_id
			qErr := db.QueryRow("SELECT id FROM findings WHERE project_id = ? AND plugin_id = ? AND plugin_source = 'nuclei'",
				projectID, entry.TemplateID).Scan(&findingID)
			if qErr == nil {
				findingCache[entry.TemplateID] = findingID
			}
		}

		if findingID == 0 {
			result, insErr := db.Exec(`INSERT INTO findings (project_id, title, severity, cvss_score, cvss_vector,
				description, solution, evidence, plugin_id, plugin_source, source, last_modified_by)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'nuclei', 'nuclei', ?)`,
				projectID, title, severity, cvssScore, cvssVector,
				description, solution, evidence, entry.TemplateID, userID)
			if insErr != nil {
				skipped++
				continue
			}
			findingID, _ = result.LastInsertId()
			findingCache[entry.TemplateID] = findingID
			findingsAdded++
		}

		// Add CVEs for this finding
		for _, cve := range cveIDs {
			db.Exec("INSERT OR IGNORE INTO finding_cves (finding_id, cve) VALUES (?, ?)", findingID, cve)
		}

		// Map finding to host with port
		port := 0
		if entry.Port != "" {
			fmt.Sscanf(entry.Port, "%d", &port)
		}
		protocol := strings.ToUpper(entry.Scheme)
		if protocol == "HTTPS" {
			protocol = "TCP"
		} else if protocol == "HTTP" {
			protocol = "TCP"
		}

		db.Exec("INSERT OR IGNORE INTO finding_hosts (finding_id, host_id, port, protocol) VALUES (?, ?, ?, ?)",
			findingID, hostID, port, protocol)

		if !matchedHostSet[hostID] {
			matchedHostSet[hostID] = true
			hostsMatched++
		}
	}

	return findingsAdded, hostsMatched, skipped, nil
}

// parseAtlasRawFile imports an Atlas raw JSON export into a project.
func parseAtlasRawFile(filePath string, projectID string, db *sql.DB, userID int) (hostsAdded, servicesAdded, findingsAdded int, err error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to read file: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid JSON: %w", err)
	}

	var isAtlas bool
	if v, ok := raw["_atlas_export"]; ok {
		json.Unmarshal(v, &isAtlas)
	}
	if !isAtlas {
		return 0, 0, 0, fmt.Errorf("not a valid Atlas raw export file")
	}

	// Import hosts
	type importHost struct {
		ID         int    `json:"id"`
		IPAddress  string `json:"ip_address"`
		Hostname   string `json:"hostname"`
		OS         string `json:"os"`
		Notes      string `json:"notes"`
		Color      string `json:"color"`
		Source     string `json:"source"`
		MACAddress string `json:"mac_address"`
		Tag        string `json:"tag"`
	}
	var hosts []importHost
	if v, ok := raw["hosts"]; ok {
		json.Unmarshal(v, &hosts)
	}

	hostIDMap := make(map[int]int)
	for _, h := range hosts {
		var existingID int
		qErr := db.QueryRow("SELECT id FROM hosts WHERE project_id = ? AND ip_address = ?", projectID, h.IPAddress).Scan(&existingID)
		if qErr == nil {
			hostIDMap[h.ID] = existingID
			db.Exec(`UPDATE hosts SET
				hostname = CASE WHEN COALESCE(hostname,'') = '' THEN ? ELSE hostname END,
				os = CASE WHEN COALESCE(os,'') = '' THEN ? ELSE os END,
				mac_address = CASE WHEN COALESCE(mac_address,'') = '' THEN ? ELSE mac_address END,
				tag = CASE WHEN COALESCE(tag,'') = '' THEN ? ELSE tag END,
				last_modified_by = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`, h.Hostname, h.OS, h.MACAddress, h.Tag, userID, existingID)
			continue
		}
		res, insErr := db.Exec(`INSERT INTO hosts (project_id, ip_address, hostname, os, notes, color, source, mac_address, tag, last_modified_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			projectID, h.IPAddress, h.Hostname, h.OS, h.Notes, h.Color, h.Source, h.MACAddress, h.Tag, userID)
		if insErr != nil {
			continue
		}
		newID, _ := res.LastInsertId()
		hostIDMap[h.ID] = int(newID)
		hostsAdded++
	}

	// Import services
	type importService struct {
		HostID      int    `json:"host_id"`
		Port        int    `json:"port"`
		Protocol    string `json:"protocol"`
		ServiceName string `json:"service_name"`
		Version     string `json:"version"`
		Color       string `json:"color"`
		Source      string `json:"source"`
	}
	var svcList []importService
	if v, ok := raw["services"]; ok {
		json.Unmarshal(v, &svcList)
	}
	for _, s := range svcList {
		newHostID, ok := hostIDMap[s.HostID]
		if !ok {
			continue
		}
		var existsID int
		qErr := db.QueryRow("SELECT id FROM services WHERE host_id = ? AND project_id = ? AND port = ? AND protocol = ?",
			newHostID, projectID, s.Port, s.Protocol).Scan(&existsID)
		if qErr == nil {
			continue
		}
		db.Exec(`INSERT INTO services (host_id, project_id, port, protocol, service_name, version, color, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			newHostID, projectID, s.Port, s.Protocol, s.ServiceName, s.Version, s.Color, s.Source)
		servicesAdded++
	}

	// Import hostnames
	type importHostname struct {
		HostID   int    `json:"host_id"`
		Hostname string `json:"hostname"`
		Source   string `json:"source"`
	}
	var hnList []importHostname
	if v, ok := raw["hostnames"]; ok {
		json.Unmarshal(v, &hnList)
	}
	for _, hn := range hnList {
		newHostID, ok := hostIDMap[hn.HostID]
		if !ok {
			continue
		}
		db.Exec("INSERT OR IGNORE INTO hostnames (host_id, project_id, hostname, source) VALUES (?, ?, ?, ?)",
			newHostID, projectID, hn.Hostname, hn.Source)
	}

	// Import findings
	type importFinding struct {
		ID           int     `json:"id"`
		Title        string  `json:"title"`
		Severity     string  `json:"severity"`
		CvssScore    float64 `json:"cvss_score"`
		CvssVector   string  `json:"cvss_vector"`
		Description  string  `json:"description"`
		Synopsis     string  `json:"synopsis"`
		Solution     string  `json:"solution"`
		Evidence     string  `json:"evidence"`
		PluginID     string  `json:"plugin_id"`
		PluginSource string  `json:"plugin_source"`
		Color        string  `json:"color"`
		Source       string  `json:"source"`
	}
	var fList []importFinding
	if v, ok := raw["findings"]; ok {
		json.Unmarshal(v, &fList)
	}
	findingIDMap := make(map[int]int)
	for _, f := range fList {
		// Derive severity from CVSS score
		severity := cvssToSeverity(f.CvssScore)

		var existingID int
		qErr := db.QueryRow("SELECT id FROM findings WHERE project_id = ? AND title = ? AND severity = ?",
			projectID, f.Title, severity).Scan(&existingID)
		if qErr == nil {
			findingIDMap[f.ID] = existingID
			continue
		}
		res, insErr := db.Exec(`INSERT INTO findings (project_id, title, severity, cvss_score, cvss_vector,
			description, synopsis, solution, evidence, plugin_id, plugin_source, color, source, last_modified_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			projectID, f.Title, severity, f.CvssScore, f.CvssVector,
			f.Description, f.Synopsis, f.Solution, f.Evidence,
			f.PluginID, f.PluginSource, f.Color, f.Source, userID)
		if insErr != nil {
			continue
		}
		newID, _ := res.LastInsertId()
		findingIDMap[f.ID] = int(newID)
		findingsAdded++
	}

	// Import finding_hosts
	type importFindingHost struct {
		FindingID    int    `json:"finding_id"`
		HostID       int    `json:"host_id"`
		Port         int    `json:"port"`
		Protocol     string `json:"protocol"`
		PluginOutput string `json:"plugin_output"`
	}
	var fhList []importFindingHost
	if v, ok := raw["finding_hosts"]; ok {
		json.Unmarshal(v, &fhList)
	}
	for _, fh := range fhList {
		newFindingID, ok1 := findingIDMap[fh.FindingID]
		newHostID, ok2 := hostIDMap[fh.HostID]
		if !ok1 || !ok2 {
			continue
		}
		db.Exec("INSERT OR IGNORE INTO finding_hosts (finding_id, host_id, port, protocol, plugin_output) VALUES (?, ?, ?, ?, ?)",
			newFindingID, newHostID, fh.Port, fh.Protocol, fh.PluginOutput)
	}

	// Import finding_cves
	type importFindingCVE struct {
		FindingID int    `json:"finding_id"`
		CVE       string `json:"cve"`
	}
	var fcList []importFindingCVE
	if v, ok := raw["finding_cves"]; ok {
		json.Unmarshal(v, &fcList)
	}
	for _, fc := range fcList {
		newFindingID, ok := findingIDMap[fc.FindingID]
		if !ok {
			continue
		}
		db.Exec("INSERT OR IGNORE INTO finding_cves (finding_id, cve) VALUES (?, ?)", newFindingID, fc.CVE)
	}

	// Import credentials
	type importCredential struct {
		Username       string `json:"username"`
		Password       string `json:"password"`
		Host           string `json:"host"`
		Service        string `json:"service"`
		CredentialType string `json:"credential_type"`
		Notes          string `json:"notes"`
	}
	var creds []importCredential
	if v, ok := raw["credentials"]; ok {
		json.Unmarshal(v, &creds)
	}
	for _, cr := range creds {
		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM credentials WHERE project_id = ? AND username = ? AND password = ? AND host = ? AND service = ?)",
			projectID, cr.Username, cr.Password, cr.Host, cr.Service).Scan(&exists)
		if exists {
			continue
		}
		db.Exec(`INSERT INTO credentials (project_id, username, password, host, service, credential_type, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			projectID, cr.Username, cr.Password, cr.Host, cr.Service, cr.CredentialType, cr.Notes)
	}

	// Import discovered_users
	var discoveredUsers []string
	if v, ok := raw["discovered_users"]; ok {
		json.Unmarshal(v, &discoveredUsers)
	}
	for _, u := range discoveredUsers {
		db.Exec("INSERT OR IGNORE INTO discovered_users (project_id, username) VALUES (?, ?)", projectID, u)
	}

	// Import web_directories
	type importWebDir struct {
		HostID     int    `json:"host_id"`
		Port       int    `json:"port"`
		BaseDomain string `json:"base_domain"`
		Path       string `json:"path"`
		Source     string `json:"source"`
	}
	var webDirs []importWebDir
	if v, ok := raw["web_directories"]; ok {
		json.Unmarshal(v, &webDirs)
	}
	for _, wd := range webDirs {
		newHostID, ok := hostIDMap[wd.HostID]
		if !ok {
			continue
		}
		db.Exec("INSERT OR IGNORE INTO web_directories (host_id, project_id, port, base_domain, path, source) VALUES (?, ?, ?, ?, ?, ?)",
			newHostID, projectID, wd.Port, wd.BaseDomain, wd.Path, wd.Source)
	}

	// Import web_probes
	type importWebProbe struct {
		HostID        int    `json:"host_id"`
		Port          int    `json:"port"`
		Scheme        string `json:"scheme"`
		URL           string `json:"url"`
		Title         string `json:"title"`
		StatusCode    int    `json:"status_code"`
		WebServer     string `json:"webserver"`
		ContentType   string `json:"content_type"`
		ContentLength int    `json:"content_length"`
		Tech          string `json:"tech"`
		Location      string `json:"location"`
		ResponseTime  string `json:"response_time"`
	}
	var webProbes []importWebProbe
	if v, ok := raw["web_probes"]; ok {
		json.Unmarshal(v, &webProbes)
	}
	for _, wp := range webProbes {
		newHostID, ok := hostIDMap[wp.HostID]
		if !ok {
			continue
		}
		db.Exec(`INSERT OR IGNORE INTO web_probes (host_id, project_id, port, scheme, url, title,
			status_code, webserver, content_type, content_length, tech, location, response_time)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newHostID, projectID, wp.Port, wp.Scheme, wp.URL, wp.Title,
			wp.StatusCode, wp.WebServer, wp.ContentType, wp.ContentLength, wp.Tech, wp.Location, wp.ResponseTime)
	}

	return hostsAdded, servicesAdded, findingsAdded, nil
}

// parseLairFile imports a LAIR framework JSON export into the project
func parseLairFile(filePath string, projectID string, db *sql.DB, userID int) (hostsAdded, servicesAdded, findingsAdded int, err error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to read file: %w", err)
	}

	// Strip UTF-8 BOM if present
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid JSON: %w", err)
	}

	// Parse hosts
	type lairOS struct {
		Tool        string `json:"tool"`
		Weight      int    `json:"weight"`
		Fingerprint string `json:"fingerprint"`
	}
	type lairService struct {
		Port           int    `json:"port"`
		Protocol       string `json:"protocol"`
		Service        string `json:"service"`
		Product        string `json:"product"`
		Status         string `json:"status"`
		LastModifiedBy string `json:"lastModifiedBy"`
	}
	type lairHost struct {
		IPv4           string        `json:"ipv4"`
		MAC            string        `json:"mac"`
		Hostnames      []string      `json:"hostnames"`
		OS             lairOS        `json:"os"`
		Status         string        `json:"status"`
		Services       []lairService `json:"services"`
		LastModifiedBy string        `json:"lastModifiedBy"`
	}

	var hosts []lairHost
	if v, ok := raw["hosts"]; ok {
		json.Unmarshal(v, &hosts)
	}

	// Build IP -> host_id map
	ipToHostID := make(map[string]int)

	for _, h := range hosts {
		if h.IPv4 == "" {
			continue
		}

		// Parse color from status (strip "lair-" prefix)
		color := "grey"
		if strings.HasPrefix(h.Status, "lair-") {
			color = strings.TrimPrefix(h.Status, "lair-")
		}
		switch color {
		case "grey", "green", "blue", "yellow", "orange", "red":
			// valid color
		default:
			color = "grey"
		}

		var existingID int
		qErr := db.QueryRow("SELECT id FROM hosts WHERE project_id = ? AND ip_address = ?", projectID, h.IPv4).Scan(&existingID)
		if qErr == nil {
			// Host exists — update empty fields
			ipToHostID[h.IPv4] = existingID
			db.Exec(`UPDATE hosts SET
				os = CASE WHEN COALESCE(os,'') = '' THEN ? ELSE os END,
				mac_address = CASE WHEN COALESCE(mac_address,'') = '' THEN ? ELSE mac_address END,
				last_modified_by = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`, h.OS.Fingerprint, h.MAC, userID, existingID)
		} else {
			// Insert new host
			res, insErr := db.Exec(`INSERT INTO hosts (project_id, ip_address, os, color, source, mac_address, last_modified_by)
				VALUES (?, ?, ?, ?, 'lair', ?, ?)`,
				projectID, h.IPv4, h.OS.Fingerprint, color, h.MAC, userID)
			if insErr != nil {
				continue
			}
			newID, _ := res.LastInsertId()
			ipToHostID[h.IPv4] = int(newID)
			hostsAdded++
		}

		hostID := ipToHostID[h.IPv4]

		// Insert hostnames
		for _, hn := range h.Hostnames {
			if hn == "" {
				continue
			}
			db.Exec("INSERT OR IGNORE INTO hostnames (host_id, project_id, hostname, source) VALUES (?, ?, ?, 'lair')",
				hostID, projectID, hn)
		}

		// Insert services
		for _, s := range h.Services {
			protocol := strings.ToUpper(s.Protocol) // LAIR uses lowercase, Atlas uses uppercase
			svcColor := "grey"
			if strings.HasPrefix(s.Status, "lair-") {
				svcColor = strings.TrimPrefix(s.Status, "lair-")
			}
			switch svcColor {
			case "grey", "green", "blue", "yellow", "orange", "red":
				// valid color
			default:
				svcColor = "grey"
			}

			var existsSvcID int
			qErr := db.QueryRow("SELECT id FROM services WHERE host_id = ? AND project_id = ? AND port = ? AND protocol = ?",
				hostID, projectID, s.Port, protocol).Scan(&existsSvcID)
			if qErr == nil {
				continue // already exists
			}
			db.Exec(`INSERT INTO services (host_id, project_id, port, protocol, service_name, version, color, source)
				VALUES (?, ?, ?, ?, ?, ?, ?, 'lair')`,
				hostID, projectID, s.Port, protocol, s.Service, s.Product, svcColor)
			servicesAdded++
		}
	}

	// Parse issues -> findings
	type lairPluginID struct {
		Tool string `json:"tool"`
		ID   string `json:"id"`
	}
	type lairIssueHost struct {
		IPv4     string `json:"ipv4"`
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
	}
	type lairIdentifiedBy struct {
		Tool string `json:"tool"`
	}
	type lairIssue struct {
		Title        string              `json:"title"`
		CVSS         float64             `json:"cvss"`
		Rating       string              `json:"rating"`
		Description  string              `json:"description"`
		Solution     string              `json:"solution"`
		Evidence     string              `json:"evidence"`
		PluginIDs    []lairPluginID      `json:"pluginIds"`
		CVEs         []string            `json:"cves"`
		Hosts        []lairIssueHost     `json:"hosts"`
		IdentifiedBy []lairIdentifiedBy  `json:"identifiedBy"`
	}

	var issues []lairIssue
	if v, ok := raw["issues"]; ok {
		json.Unmarshal(v, &issues)
	}

	for _, issue := range issues {
		if issue.Title == "" {
			continue
		}

		// Map severity
		severity := issue.Rating
		if severity == "" {
			severity = cvssToSeverity(issue.CVSS)
		}
		// Validate severity
		switch severity {
		case "critical", "high", "medium", "low", "informational":
			// ok
		default:
			severity = "informational"
		}

		// Extract plugin info
		pluginID := ""
		pluginSource := ""
		if len(issue.PluginIDs) > 0 {
			pluginSource = issue.PluginIDs[0].Tool
			pluginID = issue.PluginIDs[0].ID
		}

		// Source from identifiedBy
		source := "lair"
		if len(issue.IdentifiedBy) > 0 {
			tools := make([]string, 0, len(issue.IdentifiedBy))
			for _, ib := range issue.IdentifiedBy {
				if ib.Tool != "" {
					tools = append(tools, ib.Tool)
				}
			}
			if len(tools) > 0 {
				source = strings.Join(tools, ", ")
			}
		}

		// Dedup by title + severity
		var existingFindingID int
		qErr := db.QueryRow("SELECT id FROM findings WHERE project_id = ? AND title = ? AND severity = ?",
			projectID, issue.Title, severity).Scan(&existingFindingID)
		if qErr == nil {
			// Already exists — still add host associations
			for _, ih := range issue.Hosts {
				hostID, ok := ipToHostID[ih.IPv4]
				if !ok {
					continue
				}
				proto := strings.ToUpper(ih.Protocol)
				db.Exec("INSERT OR IGNORE INTO finding_hosts (finding_id, host_id, port, protocol) VALUES (?, ?, ?, ?)",
					existingFindingID, hostID, ih.Port, proto)
			}
			continue
		}

		res, insErr := db.Exec(`INSERT INTO findings (project_id, title, severity, cvss_score, description, solution, evidence,
			plugin_id, plugin_source, source, last_modified_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			projectID, issue.Title, severity, issue.CVSS, issue.Description, issue.Solution, issue.Evidence,
			pluginID, pluginSource, source, userID)
		if insErr != nil {
			continue
		}
		newFindingID, _ := res.LastInsertId()
		findingsAdded++

		// Insert finding_hosts
		for _, ih := range issue.Hosts {
			hostID, ok := ipToHostID[ih.IPv4]
			if !ok {
				continue
			}
			proto := strings.ToUpper(ih.Protocol)
			db.Exec("INSERT OR IGNORE INTO finding_hosts (finding_id, host_id, port, protocol) VALUES (?, ?, ?, ?)",
				int(newFindingID), hostID, ih.Port, proto)
		}

		// Insert CVEs
		for _, cve := range issue.CVEs {
			if cve == "" {
				continue
			}
			// LAIR stores CVEs without the "CVE-" prefix (e.g. "1999-0524")
			if !strings.HasPrefix(strings.ToUpper(cve), "CVE-") {
				cve = "CVE-" + cve
			}
			db.Exec("INSERT OR IGNORE INTO finding_cves (finding_id, cve) VALUES (?, ?)", int(newFindingID), cve)
		}
	}

	// Parse credentials
	type lairCredential struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Hash     string `json:"hash"`
		Host     string `json:"host"`
		Service  string `json:"service"`
	}

	var creds []lairCredential
	if v, ok := raw["credentials"]; ok {
		json.Unmarshal(v, &creds)
	}

	for _, cr := range creds {
		credType := ""
		notes := ""
		if cr.Hash != "" {
			credType = "hash"
			notes = cr.Hash
		}

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM credentials WHERE project_id = ? AND username = ? AND password = ? AND host = ? AND service = ?)",
			projectID, cr.Username, cr.Password, cr.Host, cr.Service).Scan(&exists)
		if exists {
			continue
		}
		db.Exec(`INSERT INTO credentials (project_id, username, password, host, service, credential_type, notes)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			projectID, cr.Username, cr.Password, cr.Host, cr.Service, credType, notes)
	}

	return hostsAdded, servicesAdded, findingsAdded, nil
}

// UploadFile handles file uploads
func UploadFile(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No file provided"})
			return
		}
		defer file.Close()

		// Create upload directory
		homeDir, _ := os.UserHomeDir()
		uploadDir := filepath.Join(homeDir, ".atlas", "uploads", projectID)
		os.MkdirAll(uploadDir, 0755)

		// Generate unique filename to avoid collisions
		safeName := filepath.Base(header.Filename)
		storedName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), safeName)
		storedPath := filepath.Join(uploadDir, storedName)

		// Save file to disk
		dst, err := os.Create(storedPath)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
			return
		}

		if _, err := io.Copy(dst, file); err != nil {
			dst.Close()
			os.Remove(storedPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save file"})
			return
		}
		dst.Close()

		// Auto-detect tool type
		toolType, err := detectToolType(storedPath, header.Filename)
		if err != nil {
			os.Remove(storedPath)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported or unrecognized file type"})
			return
		}

		// Insert into database
		_, err = db.Exec(`
			INSERT INTO uploads (project_id, filename, stored_path, file_size, tool_type, uploaded_by)
			VALUES (?, ?, ?, ?, ?, ?)
		`, projectID, header.Filename, storedPath, header.Size, toolType, userID)

		if err != nil {
			os.Remove(storedPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save upload record"})
			return
		}

		// Parse the uploaded file based on tool type
		var hostsAdded, servicesAdded, findingsAdded, parseSkipped int
		var parseErr error

		switch toolType {
		case "nmap":
			hostsAdded, servicesAdded, parseSkipped, parseErr = parseNmapFile(storedPath, projectID, db, userID)
		case "nessus":
			hostsAdded, servicesAdded, findingsAdded, parseSkipped, parseErr = parseNessusFile(storedPath, projectID, db, userID)
		case "bbot":
			hostsAdded, servicesAdded, parseSkipped, parseErr = parseBbotFile(storedPath, projectID, db)
		case "httpx":
			hostsAdded, servicesAdded, parseSkipped, parseErr = parseHttpxFile(storedPath, projectID, db)
		case "nuclei":
			findingsAdded, _, parseSkipped, parseErr = parseNucleiFile(storedPath, projectID, db, userID)
		case "atlas_raw":
			hostsAdded, servicesAdded, findingsAdded, parseErr = parseAtlasRawFile(storedPath, projectID, db, userID)
		case "lair":
			hostsAdded, servicesAdded, findingsAdded, parseErr = parseLairFile(storedPath, projectID, db, userID)
		}

		if parseErr != nil {
			fmt.Printf("Warning: parsing error for %s: %v\n", header.Filename, parseErr)
		}

		// Transition yellow hosts to grey if they now have services or findings
		autoColorNewHosts(db, projectID)

		c.JSON(http.StatusOK, gin.H{
			"success":         true,
			"tool_type":       toolType,
			"hosts_added":     hostsAdded,
			"services_added":  servicesAdded,
			"findings_added":  findingsAdded,
			"skipped":         parseSkipped,
		})
	}
}

// DeleteUpload removes an uploaded file
func DeleteUpload(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		uploadID := c.PostForm("upload_id")

		// Get the stored path before deleting
		var storedPath string
		err := db.QueryRow("SELECT stored_path FROM uploads WHERE id = ? AND project_id = ?", uploadID, projectID).Scan(&storedPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Upload not found"})
			return
		}

		// Delete from database
		_, err = db.Exec("DELETE FROM uploads WHERE id = ? AND project_id = ?", uploadID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete upload"})
			return
		}

		// Delete file from disk
		os.Remove(storedPath)

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func ProjectExports(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		rows, err := db.Query(`
			SELECT e.id, e.filename, e.stored_path, e.export_type, e.generated_by, e.created_at, u.username
			FROM exports e
			JOIN users u ON e.generated_by = u.id
			WHERE e.project_id = ?
			ORDER BY e.created_at DESC`, projectID)
		if err != nil {
			c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": "Failed to load exports"})
			return
		}
		defer rows.Close()

		var exports []models.ExportWithUser
		for rows.Next() {
			var e models.ExportWithUser
			rows.Scan(&e.ID, &e.Filename, &e.StoredPath, &e.ExportType, &e.GeneratedBy, &e.CreatedAt, &e.Username)
			e.ProjectID = projectID
			exports = append(exports, e)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		// Load export tags
		var exportTags []struct {
			ID   int
			Name string
		}
		tagRows, tagErr := db.Query("SELECT id, name FROM export_tags WHERE project_id = ? ORDER BY name ASC", projectID)
		if tagErr == nil {
			defer tagRows.Close()
			for tagRows.Next() {
				var t struct {
					ID   int
					Name string
				}
				tagRows.Scan(&t.ID, &t.Name)
				exportTags = append(exportTags, t)
			}
			if err := tagRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		c.HTML(http.StatusOK, "exports.html", gin.H{
			"username":     username,
			"project":      project,
			"page_type":    "exports",
			"is_admin":     isAdmin,
			"exports":      exports,
			"export_tags":  exportTags,
			"ngrok_active": IsNgrokActive(),
		})
	}
}

func DeleteExport(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		exportID := c.PostForm("export_id")

		var storedPath string
		err := db.QueryRow("SELECT stored_path FROM exports WHERE id = ? AND project_id = ?", exportID, projectID).Scan(&storedPath)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Export not found"})
			return
		}

		os.Remove(storedPath)
		db.Exec("DELETE FROM exports WHERE id = ? AND project_id = ?", exportID, projectID)

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func DownloadExport(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		exportID := c.Param("export_id")

		var storedPath, filename string
		err := db.QueryRow("SELECT stored_path, filename FROM exports WHERE id = ? AND project_id = ?", exportID, projectID).Scan(&storedPath, &filename)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Export not found"})
			return
		}

		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filename, `"`, `_`)))
		c.File(storedPath)
	}
}

// AddExportTag adds a tag (or bulk newline-separated tags) for the project.
func AddExportTag(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		raw := strings.TrimSpace(c.PostForm("name"))
		if raw == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Tag name is required"})
			return
		}

		added := 0
		lines := strings.Split(raw, "\n")
		for _, line := range lines {
			name := strings.TrimSpace(line)
			if name == "" {
				continue
			}
			_, err := db.Exec("INSERT OR IGNORE INTO export_tags (project_id, name) VALUES (?, ?)", projectID, name)
			if err == nil {
				added++
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "added": added})
	}
}

// DeleteExportTag deletes an export tag.
func DeleteExportTag(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		tagID := c.PostForm("tag_id")

		_, err := db.Exec("DELETE FROM export_tags WHERE id = ? AND project_id = ?", tagID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete tag"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// GeneratePlexTracAssets generates a PlexTrac-format assets CSV for the project.
func GeneratePlexTracAssets(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}
		exportTag := strings.TrimSpace(c.PostForm("tag"))

		var projectName string
		err := db.QueryRow("SELECT name FROM projects WHERE id = ?", projectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			return
		}

		// Query all hosts for this project
		type hostRow struct {
			ID         int
			IPAddress  string
			Hostname   string
			OS         string
			Notes      string
			MACAddress string
			Tag        string
		}
		var hosts []hostRow
		hRows, err := db.Query(`SELECT id, ip_address, COALESCE(hostname,''), COALESCE(os,''),
			COALESCE(notes,''), COALESCE(mac_address,''), COALESCE(tag,'')
			FROM hosts WHERE project_id = ? ORDER BY id`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query hosts"})
			return
		}
		defer hRows.Close()
		for hRows.Next() {
			var h hostRow
			hRows.Scan(&h.ID, &h.IPAddress, &h.Hostname, &h.OS, &h.Notes, &h.MACAddress, &h.Tag)
			hosts = append(hosts, h)
		}
		if err := hRows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		// Build host ID -> services map (Nmap-style: port/state/protocol//service//version/)
		type serviceInfo struct {
			Port        int
			Protocol    string
			ServiceName string
			Version     string
		}
		hostServices := make(map[int][]serviceInfo)
		sRows, err := db.Query(`SELECT host_id, port, COALESCE(protocol,''), COALESCE(service_name,''), COALESCE(version,'')
			FROM services WHERE project_id = ? ORDER BY host_id, port`, projectID)
		if err == nil {
			defer sRows.Close()
			for sRows.Next() {
				var hostID, port int
				var proto, svcName, version string
				sRows.Scan(&hostID, &port, &proto, &svcName, &version)
				hostServices[hostID] = append(hostServices[hostID], serviceInfo{port, proto, svcName, version})
			}
			if err := sRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Build host ID -> hostnames map
		hostHostnames := make(map[int][]string)
		hnRows, err := db.Query(`SELECT host_id, hostname FROM hostnames WHERE project_id = ? ORDER BY host_id`, projectID)
		if err == nil {
			defer hnRows.Close()
			for hnRows.Next() {
				var hID int
				var hn string
				hnRows.Scan(&hID, &hn)
				hostHostnames[hID] = append(hostHostnames[hID], hn)
			}
			if err := hnRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Generate CSV in memory
		var buf strings.Builder
		w := csv.NewWriter(&buf)

		// Header row matching PlexTrac template
		w.Write([]string{
			"name", "ip addresses", "criticality", "data owner", "physical location",
			"system owner", "ports", "tags", "description", "parent", "type",
			"host fqdn", "hostname", "host rdns", "dns name", "mac address",
			"netbios name", "total cves", "pci status", "operating system",
		})

		for _, h := range hosts {
			// Format services as PlexTrac port string: "port/open/protocol//service//version/, ..."
			var portParts []string
			for _, s := range hostServices[h.ID] {
				portStr := fmt.Sprintf("%d/open/%s//%s//%s/",
					s.Port, strings.ToLower(s.Protocol), s.ServiceName, s.Version)
				portParts = append(portParts, portStr)
			}
			portsField := strings.Join(portParts, ", ")

			// Use first hostname as the FQDN / hostname / dns_name
			fqdn := ""
			dnsName := ""
			displayHostname := h.Hostname
			allHostnames := hostHostnames[h.ID]
			if len(allHostnames) > 0 {
				fqdn = allHostnames[0]
				if displayHostname == "" {
					displayHostname = allHostnames[0]
				}
				if len(allHostnames) > 1 {
					dnsName = allHostnames[1]
				} else {
					dnsName = fqdn
				}
			}

			// Asset name: use hostname if available, otherwise IP
			name := displayHostname
			if name == "" {
				name = h.IPAddress
			}


			// Build tags field: combine export tag with host tag
			tagsField := exportTag
			if h.Tag != "" {
				if tagsField != "" {
					tagsField += "," + h.Tag
				} else {
					tagsField = h.Tag
				}
			}

			w.Write([]string{
				name,           // name
				h.IPAddress,    // ip addresses
				"",             // criticality
				"",             // data owner
				"",             // physical location
				"",             // system owner
				portsField,     // ports
				tagsField,      // tags
				h.Notes,        // description
				"",             // parent
				"",             // type
				fqdn,           // host fqdn
				displayHostname, // hostname
				"",             // host rdns
				dnsName,        // dns name
				h.MACAddress,   // mac address
				"",             // netbios name
				"",             // total cves
				"",             // pci status
				h.OS,           // operating system
			})
		}
		w.Flush()

		// Save to disk
		homeDir, _ := os.UserHomeDir()
		exportDir := filepath.Join(homeDir, ".atlas", "exports", projectID)
		os.MkdirAll(exportDir, 0755)

		filename := fmt.Sprintf("plextrac_assets_%s_%s.csv", strings.ReplaceAll(projectName, " ", "_"), time.Now().Format("20060102_150405"))
		storedName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filename)
		storedName = filepath.Base(storedName)
		storedPath := filepath.Join(exportDir, storedName)

		if err := os.WriteFile(storedPath, []byte(buf.String()), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write export file"})
			return
		}

		_, err = db.Exec(`INSERT INTO exports (project_id, filename, stored_path, export_type, generated_by) VALUES (?, ?, ?, 'plextrac_assets', ?)`,
			projectID, filename, storedPath, userID)
		if err != nil {
			os.Remove(storedPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save export record"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// GeneratePlexTracFindings generates a PlexTrac-format findings CSV for the project.
func GeneratePlexTracFindings(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}
		exportTag := strings.TrimSpace(c.PostForm("tag"))

		var projectName string
		err := db.QueryRow("SELECT name FROM projects WHERE id = ?", projectID).Scan(&projectName)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			return
		}

		// Query all findings
		type findingRow struct {
			ID          int
			Title       string
			Severity    string
			CvssScore   float64
			Description string
			Solution    string
		}
		var findings []findingRow
		fRows, err := db.Query(`SELECT id, title, severity, CAST(COALESCE(cvss_score,0) AS REAL),
			COALESCE(description,''), COALESCE(solution,'')
			FROM findings WHERE project_id = ? ORDER BY id`, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query findings"})
			return
		}
		defer fRows.Close()
		for fRows.Next() {
			var f findingRow
			fRows.Scan(&f.ID, &f.Title, &f.Severity, &f.CvssScore, &f.Description, &f.Solution)
			findings = append(findings, f)
		}
		if err := fRows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		// Build finding ID -> affected assets (host IPs with port info)
		type affectedAsset struct {
			IP       string
			Port     int
			Protocol string
		}
		findingAssets := make(map[int][]affectedAsset)
		faRows, err := db.Query(`SELECT fh.finding_id, h.ip_address, COALESCE(fh.port,0), COALESCE(fh.protocol,'')
			FROM finding_hosts fh
			JOIN hosts h ON fh.host_id = h.id
			WHERE h.project_id = ?`, projectID)
		if err == nil {
			defer faRows.Close()
			for faRows.Next() {
				var fid, port int
				var ip, proto string
				faRows.Scan(&fid, &ip, &port, &proto)
				findingAssets[fid] = append(findingAssets[fid], affectedAsset{ip, port, proto})
			}
			if err := faRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Build finding ID -> CVEs
		findingCVEs := make(map[int][]string)
		fcRows, err := db.Query(`SELECT fc.finding_id, fc.cve FROM finding_cves fc
			JOIN findings f ON fc.finding_id = f.id
			WHERE f.project_id = ?`, projectID)
		if err == nil {
			defer fcRows.Close()
			for fcRows.Next() {
				var fid int
				var cve string
				fcRows.Scan(&fid, &cve)
				findingCVEs[fid] = append(findingCVEs[fid], cve)
			}
			if err := fcRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Build finding ID -> CWEs (from plugin_source if it looks like a CWE)
		// PlexTrac uses CWE field -- we don't have a dedicated CWE table, so leave empty unless plugin_source contains CWE data

		// Generate CSV
		var buf strings.Builder
		w := csv.NewWriter(&buf)

		// Header row matching PlexTrac findings template
		w.Write([]string{
			"title", "severity", "status", "description", "recommendations",
			"references", "affected_assets", "tags", "cvss_temporal", "cwe", "cve", "category",
		})

		for _, f := range findings {
			// Map severity: capitalize first letter for PlexTrac
			sev := f.Severity
			if sev == "" {
				sev = "informational"
			}
			severity := strings.ToUpper(sev[:1]) + sev[1:]

			// Build affected_assets: comma-separated URLs/IPs
			var assetParts []string
			for _, a := range findingAssets[f.ID] {
				if a.Port > 0 {
					assetParts = append(assetParts, fmt.Sprintf("%s:%d", a.IP, a.Port))
				} else {
					assetParts = append(assetParts, a.IP)
				}
			}
			affectedAssetsField := strings.Join(assetParts, ",")

			// Build CVE field: comma-separated
			cveField := strings.Join(findingCVEs[f.ID], ",")

			// CVSS as string (always include, even 0.0 for informational)
			cvssStr := fmt.Sprintf("%.1f", f.CvssScore)

			w.Write([]string{
				f.Title,             // title
				severity,            // severity
				"Open",              // status
				f.Description,       // description
				f.Solution,          // recommendations
				"",                  // references
				affectedAssetsField, // affected_assets
				exportTag,           // tags
				cvssStr,             // cvss_temporal
				"",                  // cwe
				cveField,            // cve
				"",                  // category
			})
		}
		w.Flush()

		// Save to disk
		homeDir, _ := os.UserHomeDir()
		exportDir := filepath.Join(homeDir, ".atlas", "exports", projectID)
		os.MkdirAll(exportDir, 0755)

		filename := fmt.Sprintf("plextrac_findings_%s_%s.csv", strings.ReplaceAll(projectName, " ", "_"), time.Now().Format("20060102_150405"))
		storedName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filename)
		storedName = filepath.Base(storedName)
		storedPath := filepath.Join(exportDir, storedName)

		if err := os.WriteFile(storedPath, []byte(buf.String()), 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write export file"})
			return
		}

		_, err = db.Exec(`INSERT INTO exports (project_id, filename, stored_path, export_type, generated_by) VALUES (?, ?, ?, 'plextrac_findings', ?)`,
			projectID, filename, storedPath, userID)
		if err != nil {
			os.Remove(storedPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save export record"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// GenerateRawExport exports all project data as an Atlas raw JSON file.
func GenerateRawExport(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Query project info
		var project struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			StartDate   string `json:"start_date"`
			EndDate     string `json:"end_date"`
			CreatedAt   string `json:"created_at"`
		}
		{
			var desc, sd, ed sql.NullString
			err := db.QueryRow("SELECT id, name, COALESCE(description,''), COALESCE(start_date,''), COALESCE(end_date,''), created_at FROM projects WHERE id = ?", projectID).Scan(
				&project.ID, &project.Name, &desc, &sd, &ed, &project.CreatedAt)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
				return
			}
			project.Description = desc.String
			project.StartDate = sd.String
			project.EndDate = ed.String
		}

		// Query hosts
		type exportHost struct {
			ID         int    `json:"id"`
			IPAddress  string `json:"ip_address"`
			Hostname   string `json:"hostname"`
			OS         string `json:"os"`
			Notes      string `json:"notes"`
			Color      string `json:"color"`
			Source     string `json:"source"`
			MACAddress string `json:"mac_address"`
			Tag        string `json:"tag"`
		}
		var hosts []exportHost
		hRows, _ := db.Query(`SELECT id, ip_address, COALESCE(hostname,''), COALESCE(os,''), COALESCE(notes,''),
			COALESCE(color,'grey'), COALESCE(source,'manual'), COALESCE(mac_address,''), COALESCE(tag,'')
			FROM hosts WHERE project_id = ? ORDER BY id`, projectID)
		if hRows != nil {
			defer hRows.Close()
			for hRows.Next() {
				var h exportHost
				hRows.Scan(&h.ID, &h.IPAddress, &h.Hostname, &h.OS, &h.Notes, &h.Color, &h.Source, &h.MACAddress, &h.Tag)
				hosts = append(hosts, h)
			}
			if err := hRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query services
		type exportService struct {
			HostID      int    `json:"host_id"`
			Port        int    `json:"port"`
			Protocol    string `json:"protocol"`
			ServiceName string `json:"service_name"`
			Version     string `json:"version"`
			Color       string `json:"color"`
			Source      string `json:"source"`
		}
		var services []exportService
		sRows, _ := db.Query(`SELECT host_id, port, COALESCE(protocol,''), COALESCE(service_name,''),
			COALESCE(version,''), COALESCE(color,'grey'), COALESCE(source,'manual')
			FROM services WHERE project_id = ? ORDER BY host_id, port`, projectID)
		if sRows != nil {
			defer sRows.Close()
			for sRows.Next() {
				var s exportService
				sRows.Scan(&s.HostID, &s.Port, &s.Protocol, &s.ServiceName, &s.Version, &s.Color, &s.Source)
				services = append(services, s)
			}
			if err := sRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query hostnames
		type exportHostname struct {
			HostID   int    `json:"host_id"`
			Hostname string `json:"hostname"`
			Source   string `json:"source"`
		}
		var hostnames []exportHostname
		hnRows, _ := db.Query(`SELECT host_id, hostname, COALESCE(source,'manual')
			FROM hostnames WHERE project_id = ? ORDER BY host_id`, projectID)
		if hnRows != nil {
			defer hnRows.Close()
			for hnRows.Next() {
				var hn exportHostname
				hnRows.Scan(&hn.HostID, &hn.Hostname, &hn.Source)
				hostnames = append(hostnames, hn)
			}
			if err := hnRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query findings
		type exportFinding struct {
			ID           int     `json:"id"`
			Title        string  `json:"title"`
			Severity     string  `json:"severity"`
			CvssScore    float64 `json:"cvss_score"`
			CvssVector   string  `json:"cvss_vector"`
			Description  string  `json:"description"`
			Synopsis     string  `json:"synopsis"`
			Solution     string  `json:"solution"`
			Evidence     string  `json:"evidence"`
			PluginID     string  `json:"plugin_id"`
			PluginSource string  `json:"plugin_source"`
			Color        string  `json:"color"`
			Source       string  `json:"source"`
		}
		var findings []exportFinding
		fRows, _ := db.Query(`SELECT id, title, severity, COALESCE(cvss_score,0), COALESCE(cvss_vector,''),
			COALESCE(description,''), COALESCE(synopsis,''), COALESCE(solution,''), COALESCE(evidence,''),
			COALESCE(plugin_id,''), COALESCE(plugin_source,''), COALESCE(color,'grey'), COALESCE(source,'manual')
			FROM findings WHERE project_id = ? ORDER BY id`, projectID)
		if fRows != nil {
			defer fRows.Close()
			for fRows.Next() {
				var f exportFinding
				fRows.Scan(&f.ID, &f.Title, &f.Severity, &f.CvssScore, &f.CvssVector,
					&f.Description, &f.Synopsis, &f.Solution, &f.Evidence,
					&f.PluginID, &f.PluginSource, &f.Color, &f.Source)
				findings = append(findings, f)
			}
			if err := fRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query finding_hosts (join to get IP for portability)
		type exportFindingHost struct {
			FindingID    int    `json:"finding_id"`
			HostID       int    `json:"host_id"`
			Port         int    `json:"port"`
			Protocol     string `json:"protocol"`
			PluginOutput string `json:"plugin_output"`
		}
		var findingHosts []exportFindingHost
		fhRows, _ := db.Query(`SELECT finding_id, host_id, COALESCE(port,0), COALESCE(protocol,''), COALESCE(plugin_output,'')
			FROM finding_hosts fh
			JOIN findings f ON fh.finding_id = f.id
			WHERE f.project_id = ?`, projectID)
		if fhRows != nil {
			defer fhRows.Close()
			for fhRows.Next() {
				var fh exportFindingHost
				fhRows.Scan(&fh.FindingID, &fh.HostID, &fh.Port, &fh.Protocol, &fh.PluginOutput)
				findingHosts = append(findingHosts, fh)
			}
			if err := fhRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query finding_cves
		type exportFindingCVE struct {
			FindingID int    `json:"finding_id"`
			CVE       string `json:"cve"`
		}
		var findingCVEs []exportFindingCVE
		fcRows, _ := db.Query(`SELECT fc.finding_id, fc.cve FROM finding_cves fc
			JOIN findings f ON fc.finding_id = f.id
			WHERE f.project_id = ?`, projectID)
		if fcRows != nil {
			defer fcRows.Close()
			for fcRows.Next() {
				var fc exportFindingCVE
				fcRows.Scan(&fc.FindingID, &fc.CVE)
				findingCVEs = append(findingCVEs, fc)
			}
			if err := fcRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query credentials
		type exportCredential struct {
			Username       string `json:"username"`
			Password       string `json:"password"`
			Host           string `json:"host"`
			Service        string `json:"service"`
			CredentialType string `json:"credential_type"`
			Notes          string `json:"notes"`
		}
		var credentials []exportCredential
		cRows, _ := db.Query(`SELECT COALESCE(username,''), COALESCE(password,''), COALESCE(host,''),
			COALESCE(service,''), COALESCE(credential_type,''), COALESCE(notes,'')
			FROM credentials WHERE project_id = ?`, projectID)
		if cRows != nil {
			defer cRows.Close()
			for cRows.Next() {
				var cr exportCredential
				cRows.Scan(&cr.Username, &cr.Password, &cr.Host, &cr.Service, &cr.CredentialType, &cr.Notes)
				credentials = append(credentials, cr)
			}
			if err := cRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query discovered_users
		var discoveredUsers []string
		duRows, _ := db.Query("SELECT username FROM discovered_users WHERE project_id = ? ORDER BY username", projectID)
		if duRows != nil {
			defer duRows.Close()
			for duRows.Next() {
				var u string
				duRows.Scan(&u)
				discoveredUsers = append(discoveredUsers, u)
			}
			if err := duRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query web_directories
		type exportWebDir struct {
			HostID     int    `json:"host_id"`
			Port       int    `json:"port"`
			BaseDomain string `json:"base_domain"`
			Path       string `json:"path"`
			Source     string `json:"source"`
		}
		var webDirs []exportWebDir
		wdRows, _ := db.Query(`SELECT host_id, port, COALESCE(base_domain,''), path, COALESCE(source,'manual')
			FROM web_directories WHERE project_id = ?`, projectID)
		if wdRows != nil {
			defer wdRows.Close()
			for wdRows.Next() {
				var wd exportWebDir
				wdRows.Scan(&wd.HostID, &wd.Port, &wd.BaseDomain, &wd.Path, &wd.Source)
				webDirs = append(webDirs, wd)
			}
			if err := wdRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query web_probes
		type exportWebProbe struct {
			HostID        int    `json:"host_id"`
			Port          int    `json:"port"`
			Scheme        string `json:"scheme"`
			URL           string `json:"url"`
			Title         string `json:"title"`
			StatusCode    int    `json:"status_code"`
			WebServer     string `json:"webserver"`
			ContentType   string `json:"content_type"`
			ContentLength int    `json:"content_length"`
			Tech          string `json:"tech"`
			Location      string `json:"location"`
			ResponseTime  string `json:"response_time"`
		}
		var webProbes []exportWebProbe
		wpRows, _ := db.Query(`SELECT host_id, port, COALESCE(scheme,''), COALESCE(url,''), COALESCE(title,''),
			COALESCE(status_code,0), COALESCE(webserver,''), COALESCE(content_type,''), COALESCE(content_length,0),
			COALESCE(tech,''), COALESCE(location,''), COALESCE(response_time,'')
			FROM web_probes WHERE project_id = ?`, projectID)
		if wpRows != nil {
			defer wpRows.Close()
			for wpRows.Next() {
				var wp exportWebProbe
				wpRows.Scan(&wp.HostID, &wp.Port, &wp.Scheme, &wp.URL, &wp.Title, &wp.StatusCode,
					&wp.WebServer, &wp.ContentType, &wp.ContentLength, &wp.Tech, &wp.Location, &wp.ResponseTime)
				webProbes = append(webProbes, wp)
			}
			if err := wpRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Build export envelope
		exportData := map[string]interface{}{
			"_atlas_export":   true,
			"_version":        "1.0",
			"_exported_at":    time.Now().UTC().Format(time.RFC3339),
			"project":         project,
			"hosts":           hosts,
			"services":        services,
			"hostnames":       hostnames,
			"findings":        findings,
			"finding_hosts":   findingHosts,
			"finding_cves":    findingCVEs,
			"credentials":     credentials,
			"discovered_users": discoveredUsers,
			"web_directories": webDirs,
			"web_probes":      webProbes,
		}

		jsonBytes, err := json.MarshalIndent(exportData, "", "  ")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate JSON"})
			return
		}

		// Save file to disk
		homeDir, _ := os.UserHomeDir()
		exportDir := filepath.Join(homeDir, ".atlas", "exports", projectID)
		os.MkdirAll(exportDir, 0755)

		filename := fmt.Sprintf("atlas_raw_%s_%s.json", project.Name, time.Now().Format("20060102_150405"))
		// Sanitize filename
		filename = strings.ReplaceAll(filename, " ", "_")
		storedName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filename)
		storedName = filepath.Base(storedName)
		storedPath := filepath.Join(exportDir, storedName)

		if err := os.WriteFile(storedPath, jsonBytes, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write export file"})
			return
		}

		_, err = db.Exec(`INSERT INTO exports (project_id, filename, stored_path, export_type, generated_by) VALUES (?, ?, ?, 'atlas_raw', ?)`,
			projectID, filename, storedPath, userID)
		if err != nil {
			os.Remove(storedPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save export record"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// ipToLong converts a dotted IPv4 address to a uint32 for LAIR's longIpv4Addr field
func ipToLong(ip string) uint32 {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return 0
	}
	var result uint32
	for _, p := range parts {
		v, _ := strconv.Atoi(p)
		result = (result << 8) | uint32(v)
	}
	return result
}

// generateHexID generates a 24-character hex string for LAIR _id fields
func generateHexID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateLairExport generates a LAIR-format JSON export of the project
func GenerateLairExport(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Query project info
		var projectName, projectDesc, projectCreatedAt string
		{
			var desc sql.NullString
			err := db.QueryRow("SELECT name, COALESCE(description,''), created_at FROM projects WHERE id = ?", projectID).Scan(
				&projectName, &desc, &projectCreatedAt)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
				return
			}
			projectDesc = desc.String
		}

		// Format createdAt as MM/DD/YYYY
		createdAt := projectCreatedAt
		if t, err := time.Parse("2006-01-02 15:04:05", projectCreatedAt); err == nil {
			createdAt = t.Format("01/02/2006")
		} else if t, err := time.Parse(time.RFC3339, projectCreatedAt); err == nil {
			createdAt = t.Format("01/02/2006")
		}

		// Query all hosts with their services and hostnames
		type dbHost struct {
			ID         int
			IPAddress  string
			OS         string
			Color      string
			MACAddress string
		}
		var hosts []dbHost
		hRows, _ := db.Query(`SELECT id, ip_address, COALESCE(os,''), COALESCE(color,'grey'), COALESCE(mac_address,'')
			FROM hosts WHERE project_id = ? ORDER BY id`, projectID)
		if hRows != nil {
			defer hRows.Close()
			for hRows.Next() {
				var h dbHost
				hRows.Scan(&h.ID, &h.IPAddress, &h.OS, &h.Color, &h.MACAddress)
				hosts = append(hosts, h)
			}
			if err := hRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Build host ID -> IP map for findings
		hostIDToIP := make(map[int]string)
		for _, h := range hosts {
			hostIDToIP[h.ID] = h.IPAddress
		}

		// Query hostnames per host
		hostnamesMap := make(map[int][]string)
		hnRows, _ := db.Query("SELECT host_id, hostname FROM hostnames WHERE project_id = ?", projectID)
		if hnRows != nil {
			defer hnRows.Close()
			for hnRows.Next() {
				var hostID int
				var hostname string
				hnRows.Scan(&hostID, &hostname)
				hostnamesMap[hostID] = append(hostnamesMap[hostID], hostname)
			}
			if err := hnRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query services per host
		type dbService struct {
			HostID      int
			Port        int
			Protocol    string
			ServiceName string
			Version     string
			Color       string
		}
		servicesMap := make(map[int][]dbService)
		sRows, _ := db.Query(`SELECT host_id, port, COALESCE(protocol,''), COALESCE(service_name,''),
			COALESCE(version,''), COALESCE(color,'grey')
			FROM services WHERE project_id = ? ORDER BY host_id, port`, projectID)
		if sRows != nil {
			defer sRows.Close()
			for sRows.Next() {
				var s dbService
				sRows.Scan(&s.HostID, &s.Port, &s.Protocol, &s.ServiceName, &s.Version, &s.Color)
				servicesMap[s.HostID] = append(servicesMap[s.HostID], s)
			}
			if err := sRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Build LAIR hosts array
		lairHosts := []map[string]interface{}{}
		for _, h := range hosts {
			hostHexID := generateHexID()
			status := "lair-" + h.Color

			hnList := hostnamesMap[h.ID]
			if hnList == nil {
				hnList = []string{}
			}

			// Build services array for this host
			lairServices := []map[string]interface{}{}
			for _, s := range servicesMap[h.ID] {
				svcStatus := "lair-" + s.Color
				lairSvc := map[string]interface{}{
					"_id":            generateHexID(),
					"projectId":      projectID,
					"hostId":         hostHexID,
					"port":           s.Port,
					"protocol":       strings.ToLower(s.Protocol),
					"service":        s.ServiceName,
					"product":        s.Version,
					"status":         svcStatus,
					"isFlagged":      false,
					"lastModifiedBy": "",
					"notes":          []interface{}{},
					"files":          []interface{}{},
				}
				lairServices = append(lairServices, lairSvc)
			}

			lairHost := map[string]interface{}{
				"_id":            hostHexID,
				"projectId":      projectID,
				"longIpv4Addr":   ipToLong(h.IPAddress),
				"ipv4":           h.IPAddress,
				"mac":            h.MACAddress,
				"hostnames":      hnList,
				"os":             map[string]interface{}{"tool": "", "weight": 0, "fingerprint": h.OS},
				"notes":          []interface{}{},
				"statusMessage":  "",
				"tags":           []interface{}{},
				"status":         status,
				"lastModifiedBy": "",
				"isFlagged":      false,
				"files":          []interface{}{},
				"webdirectories": []interface{}{},
				"services":       lairServices,
				"webDirectories": []interface{}{},
			}
			lairHosts = append(lairHosts, lairHost)
		}

		// Query findings with their hosts and CVEs
		type dbFinding struct {
			ID           int
			Title        string
			Severity     string
			CvssScore    float64
			Description  string
			Solution     string
			Evidence     string
			PluginID     string
			PluginSource string
			Source       string
		}
		var findings []dbFinding
		fRows, _ := db.Query(`SELECT id, title, severity, COALESCE(cvss_score,0),
			COALESCE(description,''), COALESCE(solution,''), COALESCE(evidence,''),
			COALESCE(plugin_id,''), COALESCE(plugin_source,''), COALESCE(source,'manual')
			FROM findings WHERE project_id = ? ORDER BY id`, projectID)
		if fRows != nil {
			defer fRows.Close()
			for fRows.Next() {
				var f dbFinding
				fRows.Scan(&f.ID, &f.Title, &f.Severity, &f.CvssScore,
					&f.Description, &f.Solution, &f.Evidence,
					&f.PluginID, &f.PluginSource, &f.Source)
				findings = append(findings, f)
			}
			if err := fRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query finding_hosts
		type dbFindingHost struct {
			FindingID int
			HostID    int
			Port      int
			Protocol  string
		}
		findingHostsMap := make(map[int][]dbFindingHost)
		fhRows, _ := db.Query(`SELECT fh.finding_id, fh.host_id, COALESCE(fh.port,0), COALESCE(fh.protocol,'')
			FROM finding_hosts fh JOIN findings f ON fh.finding_id = f.id
			WHERE f.project_id = ?`, projectID)
		if fhRows != nil {
			defer fhRows.Close()
			for fhRows.Next() {
				var fh dbFindingHost
				fhRows.Scan(&fh.FindingID, &fh.HostID, &fh.Port, &fh.Protocol)
				findingHostsMap[fh.FindingID] = append(findingHostsMap[fh.FindingID], fh)
			}
			if err := fhRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query finding_cves
		findingCVEsMap := make(map[int][]string)
		fcRows, _ := db.Query(`SELECT fc.finding_id, fc.cve FROM finding_cves fc
			JOIN findings f ON fc.finding_id = f.id WHERE f.project_id = ?`, projectID)
		if fcRows != nil {
			defer fcRows.Close()
			for fcRows.Next() {
				var findingID int
				var cve string
				fcRows.Scan(&findingID, &cve)
				findingCVEsMap[findingID] = append(findingCVEsMap[findingID], cve)
			}
			if err := fcRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Build LAIR issues array
		lairIssues := []map[string]interface{}{}
		for _, f := range findings {
			// Build pluginIds
			pluginIDs := []map[string]interface{}{}
			if f.PluginID != "" || f.PluginSource != "" {
				pluginIDs = append(pluginIDs, map[string]interface{}{
					"tool": f.PluginSource,
					"id":   f.PluginID,
				})
			}

			// Build hosts array
			issueHosts := []map[string]interface{}{}
			for _, fh := range findingHostsMap[f.ID] {
				ip := hostIDToIP[fh.HostID]
				if ip == "" {
					continue
				}
				issueHosts = append(issueHosts, map[string]interface{}{
					"ipv4":     ip,
					"port":     fh.Port,
					"protocol": strings.ToLower(fh.Protocol),
				})
			}

			// CVEs — LAIR stores without "CVE-" prefix
			rawCVEs := findingCVEsMap[f.ID]
			cves := make([]string, 0, len(rawCVEs))
			for _, c := range rawCVEs {
				cves = append(cves, strings.TrimPrefix(c, "CVE-"))
			}

			// identifiedBy — LAIR uses [{"tool": "name"}] format
			identifiedBy := []map[string]interface{}{}
			if f.Source != "" && f.Source != "manual" {
				for _, tool := range strings.Split(f.Source, ", ") {
					identifiedBy = append(identifiedBy, map[string]interface{}{"tool": tool})
				}
			}

			lairIssue := map[string]interface{}{
				"_id":            generateHexID(),
				"projectId":      projectID,
				"title":          f.Title,
				"cvss":           f.CvssScore,
				"rating":         f.Severity,
				"isConfirmed":    false,
				"description":    f.Description,
				"solution":       f.Solution,
				"evidence":       f.Evidence,
				"pluginIds":      pluginIDs,
				"cves":           cves,
				"references":     []interface{}{},
				"hosts":          issueHosts,
				"identifiedBy":   identifiedBy,
				"status":         "lair-grey",
				"lastModifiedBy": "",
				"isFlagged":      false,
				"notes":          []interface{}{},
				"files":          []interface{}{},
				"tags":           []interface{}{},
			}
			lairIssues = append(lairIssues, lairIssue)
		}

		// Query credentials
		type dbCredential struct {
			Username       string
			Password       string
			Host           string
			Service        string
			CredentialType string
			Notes          string
		}
		var creds []dbCredential
		cRows, _ := db.Query(`SELECT COALESCE(username,''), COALESCE(password,''), COALESCE(host,''),
			COALESCE(service,''), COALESCE(credential_type,''), COALESCE(notes,'')
			FROM credentials WHERE project_id = ?`, projectID)
		if cRows != nil {
			defer cRows.Close()
			for cRows.Next() {
				var cr dbCredential
				cRows.Scan(&cr.Username, &cr.Password, &cr.Host, &cr.Service, &cr.CredentialType, &cr.Notes)
				creds = append(creds, cr)
			}
			if err := cRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Build LAIR credentials array
		lairCreds := []map[string]interface{}{}
		for _, cr := range creds {
			hash := ""
			if cr.CredentialType == "hash" {
				hash = cr.Notes
			}
			lairCred := map[string]interface{}{
				"_id":      generateHexID(),
				"username": cr.Username,
				"password": cr.Password,
				"hash":     hash,
				"host":     cr.Host,
				"service":  cr.Service,
			}
			lairCreds = append(lairCreds, lairCred)
		}

		// Build the full LAIR export document
		lairExport := map[string]interface{}{
			"_id":            projectID,
			"name":           projectName,
			"industry":       "",
			"createdAt":      createdAt,
			"description":    projectDesc,
			"owner":          "",
			"contributors":   []interface{}{},
			"commands": []map[string]interface{}{
				{"tool": "atlas", "command": "Atlas LAIR export"},
			},
			"notes":          []interface{}{},
			"droneLog":       []interface{}{},
			"tool":           "atlas",
			"hosts":          lairHosts,
			"issues":         lairIssues,
			"credentials":    lairCreds,
			"authinterfaces": []interface{}{},
			"netblocks":      []interface{}{},
			"people":         []interface{}{},
			"files":          []interface{}{},
			"authInterfaces": []interface{}{},
		}

		jsonBytes, err := json.MarshalIndent(lairExport, "", "  ")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate LAIR JSON"})
			return
		}

		// Save file
		homeDir, _ := os.UserHomeDir()
		exportDir := filepath.Join(homeDir, ".atlas", "exports", projectID)
		os.MkdirAll(exportDir, 0755)

		filename := fmt.Sprintf("lair_%s_%s.json", projectName, time.Now().Format("20060102_150405"))
		filename = strings.ReplaceAll(filename, " ", "_")
		storedName := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filename)
		storedName = filepath.Base(storedName)
		storedPath := filepath.Join(exportDir, storedName)

		if err := os.WriteFile(storedPath, jsonBytes, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write export file"})
			return
		}

		_, err = db.Exec(`INSERT INTO exports (project_id, filename, stored_path, export_type, generated_by) VALUES (?, ?, ?, 'lair', ?)`,
			projectID, filename, storedPath, userID)
		if err != nil {
			os.Remove(storedPath)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save export record"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func ProjectSettings(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		// Check if user is admin
		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		// Get project details
		var project models.Project
		var startDate, endDate sql.NullString
		err := db.QueryRow(`
			SELECT id, name, description, start_date, end_date, creator_id, created_at, updated_at
			FROM projects WHERE id = ?
		`, projectID).Scan(&project.ID, &project.Name, &project.Description, &startDate, &endDate, &project.CreatorID, &project.CreatedAt, &project.UpdatedAt)

		if err != nil {
			c.HTML(http.StatusNotFound, "error.html", gin.H{
				"error": "Project not found",
			})
			return
		}

		if startDate.Valid {
			project.StartDate = startDate.String
		}
		if endDate.Valid {
			project.EndDate = endDate.String
		}

		// Check if current user is the creator
		isCreator := project.CreatorID == userID

		// Get project users
		rows, err := db.Query(`
			SELECT pu.id, pu.project_id, pu.user_id, u.username, pu.role, pu.created_at
			FROM project_users pu
			JOIN users u ON pu.user_id = u.id
			WHERE pu.project_id = ?
			ORDER BY pu.created_at ASC
		`, projectID)

		var projectUsers []models.ProjectUser
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var pu models.ProjectUser
				rows.Scan(&pu.ID, &pu.ProjectID, &pu.UserID, &pu.Username, &pu.Role, &pu.CreatedAt)
				projectUsers = append(projectUsers, pu)
			}
			if err := rows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Get all users for the add user dropdown
		allUsers := []models.User{}
		userRows, err := db.Query("SELECT id, username FROM users ORDER BY username")
		if err == nil {
			defer userRows.Close()
			for userRows.Next() {
				var u models.User
				userRows.Scan(&u.ID, &u.Username)
				allUsers = append(allUsers, u)
			}
			if err := userRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		c.HTML(http.StatusOK, "settings.html", gin.H{
			"username":      username,
			"project":       project,
			"is_admin":      isAdmin,
			"is_creator":    isCreator,
			"project_users": projectUsers,
			"all_users":     allUsers,
			"page_type":     "settings",
			"ngrok_active":  IsNgrokActive(),
		})
	}
}

func AddUserToProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		addUserID := c.PostForm("user_id")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Verify the requesting user is the project creator
		var creatorID sql.NullInt64
		db.QueryRow("SELECT creator_id FROM projects WHERE id = ?", projectID).Scan(&creatorID)
		if !creatorID.Valid || int(creatorID.Int64) != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only the project creator can add users"})
			return
		}

		if addUserID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
			return
		}

		// Check if user is already in project
		var exists bool
		err := db.QueryRow(`
			SELECT EXISTS(SELECT 1 FROM project_users WHERE project_id = ? AND user_id = ?)
		`, projectID, addUserID).Scan(&exists)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check user"})
			return
		}

		if exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "User already has access to this project"})
			return
		}

		// Add user to project
		_, err = db.Exec(`
			INSERT INTO project_users (project_id, user_id, role)
			VALUES (?, ?, 'member')
		`, projectID, addUserID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add user"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func RemoveUserFromProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		removeUserID := c.PostForm("user_id")

		currentUserID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Get project creator
		var creatorID sql.NullInt64
		db.QueryRow("SELECT creator_id FROM projects WHERE id = ?", projectID).Scan(&creatorID)

		// Only creator can remove users
		if !creatorID.Valid || int(creatorID.Int64) != currentUserID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only the project creator can remove users"})
			return
		}

		// Can't remove the creator
		if removeUserID == fmt.Sprint(creatorID.Int64) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot remove the project creator"})
			return
		}

		_, err := db.Exec("DELETE FROM project_users WHERE project_id = ? AND user_id = ?", projectID, removeUserID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove user"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func DeleteProject(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		confirmID := c.PostForm("confirm_id")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		// Verify confirmation
		if confirmID != projectID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Project ID confirmation does not match"})
			return
		}

		// Get project creator
		var creatorID sql.NullInt64
		err := db.QueryRow("SELECT creator_id FROM projects WHERE id = ?", projectID).Scan(&creatorID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			return
		}

		// Only creator can delete
		if !creatorID.Valid || int(creatorID.Int64) != userID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Only the project creator can delete this project"})
			return
		}

		// Delete project (CASCADE will delete related records)
		_, err = db.Exec("DELETE FROM projects WHERE id = ?", projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete project"})
			return
		}

		// Clean up uploaded and exported files
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			os.RemoveAll(filepath.Join(homeDir, ".atlas", "uploads", projectID))
			os.RemoveAll(filepath.Join(homeDir, ".atlas", "exports", projectID))
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// Helper function to handle nullable strings
func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// GetServiceHosts returns hosts associated with a specific service (all 4 columns)
func GetServiceHosts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		port := c.Query("port")
		protocol := c.Query("protocol")
		serviceName := c.Query("service")
		banner := c.Query("banner")

		rows, err := db.Query(`
			SELECT DISTINCT h.id, h.ip_address, h.hostname, h.os
			FROM services s
			INNER JOIN hosts h ON s.host_id = h.id
			WHERE s.project_id = ?
			  AND s.port = ?
			  AND (s.protocol = ? OR (s.protocol IS NULL AND ? = ''))
			  AND (s.service_name = ? OR (s.service_name IS NULL AND ? = ''))
			  AND (s.version = ? OR (s.version IS NULL AND ? = ''))
		`, projectID, port, protocol, protocol, serviceName, serviceName, banner, banner)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load hosts"})
			return
		}
		defer rows.Close()

		var hosts []models.ServiceHost
		for rows.Next() {
			var h models.ServiceHost
			var hostname, osName sql.NullString
			rows.Scan(&h.HostID, &h.IPAddress, &hostname, &osName)
			h.Hostname = hostname.String
			h.OS = osName.String
			hosts = append(hosts, h)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		sort.Slice(hosts, func(i, j int) bool {
			return ipToSortKey(hosts[i].IPAddress) < ipToSortKey(hosts[j].IPAddress)
		})

		c.JSON(http.StatusOK, gin.H{"hosts": hosts})
	}
}

// DeleteService removes all services matching all 4 columns
func DeleteService(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		port := c.PostForm("port")
		protocol := c.PostForm("protocol")
		serviceName := c.PostForm("service")
		banner := c.PostForm("banner")

		_, err := db.Exec(`
			DELETE FROM services
			WHERE project_id = ?
			  AND port = ?
			  AND (protocol = ? OR (protocol IS NULL AND ? = ''))
			  AND (service_name = ? OR (service_name IS NULL AND ? = ''))
			  AND (version = ? OR (version IS NULL AND ? = ''))
		`, projectID, port, protocol, protocol, serviceName, serviceName, banner, banner)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete service"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// BulkDeleteServices removes multiple service groups by all 4 columns
func BulkDeleteServices(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")

		var services []struct {
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
			Service  string `json:"service"`
			Banner   string `json:"banner"`
		}

		if err := c.ShouldBindJSON(&services); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		deleted := 0
		for _, svc := range services {
			result, err := db.Exec(`
				DELETE FROM services
				WHERE project_id = ?
				  AND port = ?
				  AND (protocol = ? OR (protocol IS NULL AND ? = ''))
				  AND (service_name = ? OR (service_name IS NULL AND ? = ''))
				  AND (version = ? OR (version IS NULL AND ? = ''))
			`, projectID, svc.Port, svc.Protocol, svc.Protocol, svc.Service, svc.Service, svc.Banner, svc.Banner)

			if err == nil {
				affected, _ := result.RowsAffected()
				deleted += int(affected)
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

// normalizeCredHost strips scheme, trailing slashes, and port from a credential host field
func normalizeCredHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	// Strip scheme
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimPrefix(h, "http://")
	// Strip trailing slashes
	h = strings.TrimRight(h, "/")
	// Strip port suffix (e.g. :8080)
	if idx := strings.LastIndex(h, ":"); idx > 0 {
		potentialPort := h[idx+1:]
		if _, err := strconv.Atoi(potentialPort); err == nil {
			h = h[:idx]
		}
	}
	return h
}

// AddWebDirectory adds a web directory entry to a host
func AddWebDirectory(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		portStr := strings.TrimSpace(c.PostForm("port"))
		baseDomain := strings.TrimSpace(c.PostForm("base_domain"))
		path := strings.TrimSpace(c.PostForm("path"))

		if portStr == "" || path == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Port and path are required"})
			return
		}

		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid port number (1-65535)"})
			return
		}

		_, err = db.Exec("INSERT INTO web_directories (host_id, project_id, port, base_domain, path, source) VALUES (?, ?, ?, ?, ?, 'manual')",
			hostID, projectID, port, baseDomain, path)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Directory already exists for this host/port/path"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// DeleteWebDirectory removes a single web directory entry
func DeleteWebDirectory(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		webdirID := c.PostForm("webdir_id")

		_, err := db.Exec("DELETE FROM web_directories WHERE id = ? AND host_id = ? AND project_id = ?",
			webdirID, hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete web directory"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// BulkDeleteWebDirectories removes multiple web directory entries
func BulkDeleteWebDirectories(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM web_directories WHERE id = ? AND host_id = ? AND project_id = ?",
				id, hostID, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

// AddWebProbe adds a web probe result to a host
func AddWebProbe(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		port := c.PostForm("port")
		scheme := c.PostForm("scheme")
		title := c.PostForm("title")
		statusCode := c.PostForm("status_code")
		webserver := c.PostForm("webserver")
		tech := c.PostForm("tech")
		location := c.PostForm("location")

		if port == "" || scheme == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Port and scheme are required"})
			return
		}

		_, err := db.Exec(`INSERT INTO web_probes (host_id, project_id, port, scheme, title, status_code, webserver, tech, location, source)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'manual')`,
			hostID, projectID, port, scheme, title, statusCode, webserver, tech, location)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				c.JSON(http.StatusConflict, gin.H{"error": "A web probe already exists for this port/scheme"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add web probe"})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// DeleteWebProbe removes a single web probe
func DeleteWebProbe(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		probeID := c.PostForm("probe_id")

		_, err := db.Exec("DELETE FROM web_probes WHERE id = ? AND host_id = ? AND project_id = ?",
			probeID, hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete web probe"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// BulkDeleteWebProbes removes multiple web probes
func BulkDeleteWebProbes(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM web_probes WHERE id = ? AND host_id = ? AND project_id = ?",
				id, hostID, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

// MergeServices combines multiple service entries into one
func MergeServices(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")

		var req struct {
			Services []struct {
				Port     int    `json:"port"`
				Protocol string `json:"protocol"`
				Service  string `json:"service"`
				Banner   string `json:"banner"`
			} `json:"services"`
			TargetService string `json:"target_service"`
			TargetBanner  string `json:"target_banner"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
			return
		}

		if len(req.Services) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Need at least 2 services to merge"})
			return
		}

		// Update all matching service records to use the target service name and banner
		updated := 0
		for _, svc := range req.Services {
			result, err := db.Exec(`
				UPDATE services
				SET service_name = ?, version = ?
				WHERE project_id = ?
				  AND port = ?
				  AND (protocol = ? OR (protocol IS NULL AND ? = ''))
				  AND (service_name = ? OR (service_name IS NULL AND ? = ''))
				  AND (version = ? OR (version IS NULL AND ? = ''))
			`, req.TargetService, req.TargetBanner,
				projectID, svc.Port, svc.Protocol, svc.Protocol,
				svc.Service, svc.Service, svc.Banner, svc.Banner)

			if err == nil {
				affected, _ := result.RowsAffected()
				updated += int(affected)
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "updated": updated})
	}
}

// ===== Findings Handlers =====

func AddFinding(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		title := strings.TrimSpace(c.PostForm("title"))
		cvssStr := strings.TrimSpace(c.PostForm("cvss_score"))
		synopsis := strings.TrimSpace(c.PostForm("synopsis"))
		description := strings.TrimSpace(c.PostForm("description"))
		solution := strings.TrimSpace(c.PostForm("solution"))

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		if title == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Title is required"})
			return
		}

		var cvssScore float64
		if cvssStr != "" {
			fmt.Sscanf(cvssStr, "%f", &cvssScore)
		}
		if cvssScore < 0 {
			cvssScore = 0
		} else if cvssScore > 10 {
			cvssScore = 10
		}
		severity := cvssToSeverity(cvssScore)

		_, err := db.Exec(`INSERT INTO findings (project_id, title, severity, cvss_score, synopsis, description, solution, source, last_modified_by)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'manual', ?)`,
			projectID, title, severity, cvssScore, synopsis, description, solution, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add finding"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func DeleteFinding(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.PostForm("finding_id")

		_, err := db.Exec("DELETE FROM findings WHERE id = ? AND project_id = ?", findingID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete finding"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkDeleteFindings(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM findings WHERE id = ? AND project_id = ?", id, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

func UpdateFindingColor(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.PostForm("finding_id")
		color := c.PostForm("color")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		validColors := map[string]bool{
			"grey": true, "green": true, "blue": true,
			"yellow": true, "orange": true, "red": true,
		}
		if !validColors[color] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid color"})
			return
		}

		_, err := db.Exec(
			"UPDATE findings SET color = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?",
			color, userID, findingID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update color"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkUpdateFindingColor(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		idsStr := c.PostForm("ids")
		color := c.PostForm("color")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		validColors := map[string]bool{
			"grey": true, "green": true, "blue": true,
			"yellow": true, "orange": true, "red": true,
		}
		if !validColors[color] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid color"})
			return
		}

		ids := strings.Split(idsStr, ",")
		updated := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec(
				"UPDATE findings SET color = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?",
				color, userID, id, projectID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					updated++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "updated": updated})
	}
}

// FindingDetail shows the finding detail page
func FindingDetail(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		session := sessions.Default(c)
		username := session.Get("username")
		userID, ok := getUserID(c)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		var isAdmin bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ? AND username = 'admin')", userID).Scan(&isAdmin)

		var project models.Project
		db.QueryRow("SELECT id, name FROM projects WHERE id = ?", projectID).Scan(&project.ID, &project.Name)

		// Query finding with user join
		var f models.FindingWithUser
		err := db.QueryRow(`
			SELECT f.id, f.project_id, f.title, f.severity,
				   COALESCE(f.cvss_score, 0), COALESCE(f.cvss_vector, ''),
				   COALESCE(f.description, ''), COALESCE(f.synopsis, ''),
				   COALESCE(f.solution, ''), COALESCE(f.evidence, ''),
				   COALESCE(f.plugin_id, ''), COALESCE(f.plugin_source, ''),
				   COALESCE(f.color, 'grey'), COALESCE(f.source, 'manual'),
				   COALESCE(u.username, ''),
				   (SELECT COUNT(*) FROM finding_hosts fh WHERE fh.finding_id = f.id),
				   f.created_at, f.updated_at
			FROM findings f
			LEFT JOIN users u ON f.last_modified_by = u.id
			WHERE f.id = ? AND f.project_id = ?
		`, findingID, projectID).Scan(&f.ID, &f.ProjectID, &f.Title, &f.Severity,
			&f.CvssScore, &f.CvssVector,
			&f.Description, &f.Synopsis,
			&f.Solution, &f.Evidence,
			&f.PluginID, &f.PluginSource,
			&f.Color, &f.Source,
			&f.ModifiedByUsername, &f.HostCount,
			&f.CreatedAt, &f.UpdatedAt)
		if err != nil {
			c.HTML(http.StatusNotFound, "error.html", gin.H{"error": "Finding not found"})
			return
		}

		// Query affected hosts
		var affectedHosts []models.FindingHost
		ahRows, ahErr := db.Query(`
			SELECT fh.id, fh.finding_id, fh.host_id, COALESCE(fh.port, 0), COALESCE(fh.protocol, ''),
				   COALESCE(fh.plugin_output, ''), h.ip_address, COALESCE(h.hostname, '')
			FROM finding_hosts fh
			JOIN hosts h ON fh.host_id = h.id
			WHERE fh.finding_id = ?
			ORDER BY h.ip_address ASC
		`, findingID)
		if ahErr == nil {
			defer ahRows.Close()
			for ahRows.Next() {
				var ah models.FindingHost
				if err := ahRows.Scan(&ah.ID, &ah.FindingID, &ah.HostID, &ah.Port, &ah.Protocol,
					&ah.PluginOutput, &ah.IPAddress, &ah.Hostname); err != nil {
					continue
				}
				affectedHosts = append(affectedHosts, ah)
			}
			if err := ahRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query CVEs
		var cves []models.FindingCVE
		cveRows, cveErr := db.Query("SELECT id, finding_id, cve FROM finding_cves WHERE finding_id = ? ORDER BY cve ASC", findingID)
		if cveErr == nil {
			defer cveRows.Close()
			for cveRows.Next() {
				var cv models.FindingCVE
				if err := cveRows.Scan(&cv.ID, &cv.FindingID, &cv.CVE); err != nil {
					continue
				}
				cves = append(cves, cv)
			}
			if err := cveRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		// Query all project hosts for the "Add Affected Host" dropdown
		type simpleHost struct {
			ID        int    `json:"id"`
			IPAddress string `json:"ip_address"`
			Hostname  string `json:"hostname"`
		}
		var allHosts []simpleHost
		hostRows, hostErr := db.Query("SELECT id, ip_address, COALESCE(hostname, '') FROM hosts WHERE project_id = ? ORDER BY ip_address ASC", projectID)
		if hostErr == nil {
			defer hostRows.Close()
			for hostRows.Next() {
				var sh simpleHost
				if err := hostRows.Scan(&sh.ID, &sh.IPAddress, &sh.Hostname); err != nil {
					continue
				}
				allHosts = append(allHosts, sh)
			}
			if err := hostRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		allHostsJSON, _ := json.Marshal(allHosts)

		// Compute prev/next finding IDs (same sort order as list)
		var prevFindingID, nextFindingID int
		navRows, navErr := db.Query(`
			SELECT id FROM findings WHERE project_id = ?
			ORDER BY cvss_score DESC, title ASC
		`, projectID)
		if navErr == nil {
			var allIDs []int
			for navRows.Next() {
				var id int
				navRows.Scan(&id)
				allIDs = append(allIDs, id)
			}
			if err := navRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
			navRows.Close()
			for idx, id := range allIDs {
				if id == f.ID {
					if idx > 0 {
						prevFindingID = allIDs[idx-1]
					}
					if idx < len(allIDs)-1 {
						nextFindingID = allIDs[idx+1]
					}
					break
				}
			}
		}

		c.HTML(http.StatusOK, "finding_detail.html", gin.H{
			"username":             username,
			"project":              project,
			"page_type":            "findings",
			"is_admin":             isAdmin,
			"finding":              f,
			"affected_hosts":       affectedHosts,
			"cves":                 cves,
			"all_hosts":            allHosts,
			"all_hosts_json":       string(allHostsJSON),
			"prev_finding_id":      prevFindingID,
			"next_finding_id":      nextFindingID,
			"modified_by_username": f.ModifiedByUsername,
			"ngrok_active":         IsNgrokActive(),
		})
	}
}

func UpdateFinding(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		field := strings.TrimSpace(c.PostForm("field"))
		value := c.PostForm("value")

		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		allowedFields := map[string]bool{
			"title": true, "description": true, "synopsis": true,
			"solution": true, "evidence": true, "cvss_score": true,
		}
		if !allowedFields[field] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid field"})
			return
		}

		// When CVSS score changes, auto-update severity
		if field == "cvss_score" {
			var cvssScore float64
			fmt.Sscanf(value, "%f", &cvssScore)
			if cvssScore < 0 {
				cvssScore = 0
			} else if cvssScore > 10 {
				cvssScore = 10
			}
			severity := cvssToSeverity(cvssScore)
			_, err := db.Exec("UPDATE findings SET cvss_score = ?, severity = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?",
				cvssScore, severity, userID, findingID, projectID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update finding"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "severity": severity})
			return
		}

		var err error
		switch field {
		case "title":
			_, err = db.Exec("UPDATE findings SET title = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?", value, userID, findingID, projectID)
		case "description":
			_, err = db.Exec("UPDATE findings SET description = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?", value, userID, findingID, projectID)
		case "solution":
			_, err = db.Exec("UPDATE findings SET solution = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?", value, userID, findingID, projectID)
		case "evidence":
			_, err = db.Exec("UPDATE findings SET evidence = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?", value, userID, findingID, projectID)
		case "synopsis":
			_, err = db.Exec("UPDATE findings SET synopsis = ?, last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?", value, userID, findingID, projectID)
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid field"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update finding"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func AddFindingCVE(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		cve := strings.TrimSpace(c.PostForm("cve"))

		if cve == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "CVE is required"})
			return
		}

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM findings WHERE id = ? AND project_id = ?)", findingID, projectID).Scan(&exists)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
			return
		}

		_, err := db.Exec("INSERT OR IGNORE INTO finding_cves (finding_id, cve) VALUES (?, ?)", findingID, cve)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add CVE"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func DeleteFindingCVE(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		cveID := c.PostForm("cve_id")

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM findings WHERE id = ? AND project_id = ?)", findingID, projectID).Scan(&exists)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
			return
		}

		_, err := db.Exec("DELETE FROM finding_cves WHERE id = ? AND finding_id = ?", cveID, findingID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete CVE"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkDeleteFindingCVEs(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		idsStr := c.PostForm("ids")

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM findings WHERE id = ? AND project_id = ?)", findingID, projectID).Scan(&exists)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
			return
		}

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM finding_cves WHERE id = ? AND finding_id = ?", id, findingID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

func AddFindingHost(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		hostIDStr := c.PostForm("host_id")
		portStr := c.PostForm("port")
		protocol := strings.ToUpper(strings.TrimSpace(c.PostForm("protocol")))

		if hostIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Host is required"})
			return
		}

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM findings WHERE id = ? AND project_id = ?)", findingID, projectID).Scan(&exists)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
			return
		}

		var hostExists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM hosts WHERE id = ? AND project_id = ?)", hostIDStr, projectID).Scan(&hostExists)
		if !hostExists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Host does not belong to this project"})
			return
		}

		port := 0
		if portStr != "" {
			fmt.Sscanf(portStr, "%d", &port)
		}

		// Validate that the port/protocol combination exists as a service on this host
		if port > 0 {
			var serviceExists bool
			db.QueryRow("SELECT EXISTS(SELECT 1 FROM services WHERE host_id = ? AND project_id = ? AND port = ? AND protocol = ?)",
				hostIDStr, projectID, port, protocol).Scan(&serviceExists)
			if !serviceExists {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Service not found on this host"})
				return
			}
		}

		_, err := db.Exec("INSERT OR IGNORE INTO finding_hosts (finding_id, host_id, port, protocol) VALUES (?, ?, ?, ?)",
			findingID, hostIDStr, port, protocol)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add host association"})
			return
		}

		autoColorNewHosts(db, projectID)
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func DeleteFindingHost(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		findingHostID := c.PostForm("finding_host_id")

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM findings WHERE id = ? AND project_id = ?)", findingID, projectID).Scan(&exists)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
			return
		}

		_, err := db.Exec("DELETE FROM finding_hosts WHERE id = ? AND finding_id = ?", findingHostID, findingID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove host association"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

func BulkDeleteFindingHosts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		idsStr := c.PostForm("ids")

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM findings WHERE id = ? AND project_id = ?)", findingID, projectID).Scan(&exists)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
			return
		}

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM finding_hosts WHERE id = ? AND finding_id = ?", id, findingID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

// GetHostServicesJSON returns JSON services for a given host (used by finding detail add-host modal).
func GetHostServicesJSON(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		type svcEntry struct {
			ID          int    `json:"id"`
			Port        int    `json:"port"`
			Protocol    string `json:"protocol"`
			ServiceName string `json:"service_name"`
			Version     string `json:"version"`
		}

		var services []svcEntry
		rows, err := db.Query(`
			SELECT id, port, protocol, COALESCE(service_name, ''), COALESCE(version, '')
			FROM services
			WHERE host_id = ? AND project_id = ?
			AND id IN (
				SELECT id FROM services s1
				WHERE s1.host_id = ? AND s1.project_id = ?
				AND s1.id = (
					SELECT s2.id FROM services s2
					WHERE s2.host_id = s1.host_id AND s2.port = s1.port AND s2.protocol = s1.protocol
					ORDER BY LENGTH(COALESCE(s2.service_name, '')) + LENGTH(COALESCE(s2.version, '')) DESC, s2.id ASC
					LIMIT 1
				)
			)
			ORDER BY port ASC
		`, hostID, projectID, hostID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query services"})
			return
		}
		defer rows.Close()

		for rows.Next() {
			var s svcEntry
			rows.Scan(&s.ID, &s.Port, &s.Protocol, &s.ServiceName, &s.Version)
			services = append(services, s)
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		if services == nil {
			services = []svcEntry{}
		}

		c.JSON(http.StatusOK, gin.H{"services": services})
	}
}

// BulkAddFindingHosts adds multiple host associations to a finding from newline-separated entries.
func BulkAddFindingHosts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")
		entries := c.PostForm("entries")

		var exists bool
		db.QueryRow("SELECT EXISTS(SELECT 1 FROM findings WHERE id = ? AND project_id = ?)", findingID, projectID).Scan(&exists)
		if !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Finding not found"})
			return
		}

		// Pre-load all project hosts into a map: ip -> host_id
		hostMap := make(map[string]int)
		rows, err := db.Query("SELECT id, ip_address FROM hosts WHERE project_id = ?", projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load hosts"})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			var ip string
			rows.Scan(&id, &ip)
			hostMap[ip] = id
		}
		if err := rows.Err(); err != nil {
			log.Printf("Row iteration error: %v", err)
		}

		// Pre-load all services keyed by host_id -> set of "port|PROTOCOL"
		svcMap := make(map[int]map[string]bool)
		svcRows, svcErr := db.Query("SELECT host_id, port, protocol FROM services WHERE project_id = ?", projectID)
		if svcErr == nil {
			defer svcRows.Close()
			for svcRows.Next() {
				var hid, port int
				var proto string
				svcRows.Scan(&hid, &port, &proto)
				if svcMap[hid] == nil {
					svcMap[hid] = make(map[string]bool)
				}
				svcMap[hid][fmt.Sprintf("%d|%s", port, strings.ToUpper(proto))] = true
			}
			if err := svcRows.Err(); err != nil {
				log.Printf("Row iteration error: %v", err)
			}
		}

		type lineResult struct {
			Line    string `json:"line"`
			Success bool   `json:"success"`
			Reason  string `json:"reason,omitempty"`
		}

		lines := strings.Split(entries, "\n")
		var results []lineResult
		added := 0

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Parse format: host:port/protocol (port and protocol optional)
			var hostIP, portStr, protocol string

			// Split on last '/' for protocol
			if slashIdx := strings.LastIndex(line, "/"); slashIdx != -1 {
				protocol = strings.ToUpper(strings.TrimSpace(line[slashIdx+1:]))
				line = line[:slashIdx]
			}

			// Split on last ':' for port
			if colonIdx := strings.LastIndex(line, ":"); colonIdx != -1 {
				portStr = strings.TrimSpace(line[colonIdx+1:])
				hostIP = strings.TrimSpace(line[:colonIdx])
			} else {
				hostIP = strings.TrimSpace(line)
			}

			if hostIP == "" {
				results = append(results, lineResult{Line: hostIP, Success: false, Reason: "Empty host"})
				continue
			}

			// Validate port
			port := 0
			if portStr != "" {
				p, pErr := strconv.Atoi(portStr)
				if pErr != nil || p < 0 || p > 65535 {
					results = append(results, lineResult{Line: hostIP + ":" + portStr, Success: false, Reason: "Invalid port"})
					continue
				}
				port = p
			}

			// Look up host in project
			hostIDVal, found := hostMap[hostIP]
			if !found {
				results = append(results, lineResult{Line: hostIP, Success: false, Reason: "Host not found in project"})
				continue
			}

			// Validate that port/protocol exists as a service on this host (when port specified)
			if port > 0 {
				svcKey := fmt.Sprintf("%d|%s", port, protocol)
				if svcMap[hostIDVal] == nil || !svcMap[hostIDVal][svcKey] {
					displayLine := hostIP + ":" + portStr
					if protocol != "" {
						displayLine += "/" + protocol
					}
					results = append(results, lineResult{Line: displayLine, Success: false, Reason: "Service not found on this host"})
					continue
				}
			}

			_, insErr := db.Exec("INSERT OR IGNORE INTO finding_hosts (finding_id, host_id, port, protocol) VALUES (?, ?, ?, ?)",
				findingID, hostIDVal, port, protocol)
			if insErr != nil {
				results = append(results, lineResult{Line: hostIP, Success: false, Reason: "Database error"})
				continue
			}

			added++
			displayLine := hostIP
			if portStr != "" {
				displayLine += ":" + portStr
			}
			if protocol != "" {
				displayLine += "/" + protocol
			}
			results = append(results, lineResult{Line: displayLine, Success: true})
		}

		autoColorNewHosts(db, projectID)
		c.JSON(http.StatusOK, gin.H{"success": true, "added": added, "results": results})
	}
}

// RemoveHostFindingAssoc removes a single finding_host association from the host detail Issues tab.
// This only removes the host-finding link, not the finding itself.
func RemoveHostFindingAssoc(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		findingHostID := c.PostForm("finding_host_id")

		_, err := db.Exec("DELETE FROM finding_hosts WHERE id = ? AND host_id = ?", findingHostID, hostID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove finding association"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// BulkRemoveHostFindingAssocs removes multiple finding_host associations from the host detail Issues tab.
func BulkRemoveHostFindingAssocs(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")

		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}

		idsStr := c.PostForm("ids")

		ids := strings.Split(idsStr, ",")
		deleted := 0
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			result, err := db.Exec("DELETE FROM finding_hosts WHERE id = ? AND host_id = ?", id, hostID)
			if err == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					deleted++
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
	}
}

func DeleteFindingFromDetail(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		findingID := c.Param("fid")

		_, err := db.Exec("DELETE FROM findings WHERE id = ? AND project_id = ?", findingID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete finding"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}
