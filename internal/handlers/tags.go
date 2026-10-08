package handlers

import (
	"atlas/internal/models"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// maxTagLength caps a single host tag so chips stay readable and exports stay sane.
const maxTagLength = 64

// execer is satisfied by both *sql.DB and *sql.Tx.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// parseTagInput splits comma or newline separated tag input into a trimmed,
// case-insensitively de-duplicated list. It errors if any tag is too long.
func parseTagInput(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	seen := make(map[string]bool)
	var tags []string
	for _, f := range fields {
		t := strings.Join(strings.Fields(f), " ")
		if t == "" {
			continue
		}
		if len(t) > maxTagLength {
			return nil, fmt.Errorf("tag %q is longer than %d characters", t, maxTagLength)
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		tags = append(tags, t)
	}
	return tags, nil
}

// addTagsToHosts attaches every tag to every host. Hosts outside the project are ignored.
func addTagsToHosts(ex execer, projectID string, hostIDs []int64, tags []string) error {
	for _, hostID := range hostIDs {
		for _, t := range tags {
			if _, err := ex.Exec(`INSERT OR IGNORE INTO host_tags (host_id, project_id, tag)
				SELECT id, project_id, ? FROM hosts WHERE id = ? AND project_id = ?`, t, hostID, projectID); err != nil {
				return fmt.Errorf("failed to tag host %d with %q: %w", hostID, t, err)
			}
		}
	}
	return nil
}

// loadHostTags returns the tags for a single host, sorted alphabetically.
func loadHostTags(db *sql.DB, projectID string, hostID int) ([]string, error) {
	rows, err := db.Query("SELECT tag FROM host_tags WHERE host_id = ? AND project_id = ? ORDER BY tag COLLATE NOCASE", hostID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// loadProjectHostTags returns host ID -> sorted tags for every tagged host in the project.
func loadProjectHostTags(db *sql.DB, projectID string) (map[int][]string, error) {
	rows, err := db.Query("SELECT host_id, tag FROM host_tags WHERE project_id = ? ORDER BY tag COLLATE NOCASE", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make(map[int][]string)
	for rows.Next() {
		var hostID int
		var t string
		if err := rows.Scan(&hostID, &t); err != nil {
			return nil, err
		}
		tags[hostID] = append(tags[hostID], t)
	}
	return tags, rows.Err()
}

// loadProjectTagCounts returns each distinct tag in the project with its host count.
func loadProjectTagCounts(db *sql.DB, projectID string) ([]models.TagCount, error) {
	rows, err := db.Query(`SELECT MIN(tag), COUNT(DISTINCT host_id) FROM host_tags
		WHERE project_id = ? GROUP BY tag COLLATE NOCASE ORDER BY MIN(tag) COLLATE NOCASE`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var counts []models.TagCount
	for rows.Next() {
		var tc models.TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, err
		}
		counts = append(counts, tc)
	}
	return counts, rows.Err()
}

// parseHostIDs converts a comma-separated ID list from a form into int64 IDs.
func parseHostIDs(idsStr string) ([]int64, error) {
	var ids []int64
	for _, s := range strings.Split(idsStr, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid host id %q", s)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// AddHostTags adds one or more comma-separated tags to a single host.
func AddHostTags(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID, err := strconv.ParseInt(c.Param("host_id"), 10, 64)
		if err != nil || !verifyHostInProject(db, c.Param("host_id"), projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}
		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		tags, err := parseTagInput(c.PostForm("tags"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(tags) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "At least one tag is required"})
			return
		}

		if err := addTagsToHosts(db, projectID, []int64{hostID}, tags); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if _, err := db.Exec("UPDATE hosts SET last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?", userID, hostID, projectID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Tags saved but failed to update host: " + err.Error()})
			return
		}

		current, err := loadHostTags(db, projectID, int(hostID))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Tags saved but failed to reload them: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "tags": current})
	}
}

// RemoveHostTag removes a single tag from a single host.
func RemoveHostTag(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		hostID := c.Param("host_id")
		if !verifyHostInProject(db, hostID, projectID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Host not found in this project"})
			return
		}
		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		tag := strings.TrimSpace(c.PostForm("tag"))
		if tag == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Tag is required"})
			return
		}

		if _, err := db.Exec("DELETE FROM host_tags WHERE host_id = ? AND project_id = ? AND tag = ?", hostID, projectID, tag); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove tag: " + err.Error()})
			return
		}
		if _, err := db.Exec("UPDATE hosts SET last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?", userID, hostID, projectID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Tag removed but failed to update host: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true})
	}
}

// BulkTagHosts adds or removes tags across many hosts at once.
// Form fields: ids (comma-separated host IDs), tags (comma-separated), action ("add" or "remove").
func BulkTagHosts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		userID, ok := getUserID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session"})
			return
		}

		action := c.PostForm("action")
		if action != "add" && action != "remove" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Action must be 'add' or 'remove'"})
			return
		}
		ids, err := parseHostIDs(c.PostForm("ids"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No hosts selected"})
			return
		}
		tags, err := parseTagInput(c.PostForm("tags"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(tags) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "At least one tag is required"})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction: " + err.Error()})
			return
		}
		if action == "add" {
			err = addTagsToHosts(tx, projectID, ids, tags)
		} else {
			for _, id := range ids {
				for _, t := range tags {
					if _, delErr := tx.Exec("DELETE FROM host_tags WHERE host_id = ? AND project_id = ? AND tag = ?", id, projectID, t); delErr != nil {
						err = fmt.Errorf("failed to remove tag %q from host %d: %w", t, id, delErr)
						break
					}
				}
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			for _, id := range ids {
				if _, updErr := tx.Exec("UPDATE hosts SET last_modified_by = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND project_id = ?", userID, id, projectID); updErr != nil {
					err = fmt.Errorf("failed to update host %d: %w", id, updErr)
					break
				}
			}
		}
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save tags: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "hosts": len(ids), "tags": tags})
	}
}

// ExportPortHosts downloads a plain-text list of host IPs with the given port open,
// one per line, named hosts_port_<port>.txt. With exact=1 the protocol, service and
// banner query params must also match (the same grouping as the Services table).
func ExportPortHosts(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		projectID := c.Param("id")
		port, err := strconv.Atoi(c.Query("port"))
		if err != nil || port < 0 || port > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "A valid port (0-65535) is required"})
			return
		}

		query := `SELECT DISTINCT h.ip_address FROM services s
			INNER JOIN hosts h ON s.host_id = h.id
			WHERE s.project_id = ? AND s.port = ?`
		args := []any{projectID, port}
		if c.Query("exact") == "1" {
			protocol, serviceName, banner := c.Query("protocol"), c.Query("service"), c.Query("banner")
			query += ` AND (s.protocol = ? OR (s.protocol IS NULL AND ? = ''))
				AND (s.service_name = ? OR (s.service_name IS NULL AND ? = ''))
				AND (s.version = ? OR (s.version IS NULL AND ? = ''))`
			args = append(args, protocol, protocol, serviceName, serviceName, banner, banner)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query hosts: " + err.Error()})
			return
		}
		defer rows.Close()
		var ips []string
		for rows.Next() {
			var ip string
			if err := rows.Scan(&ip); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read hosts: " + err.Error()})
				return
			}
			ips = append(ips, ip)
		}
		if err := rows.Err(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read hosts: " + err.Error()})
			return
		}
		if len(ips) == 0 {
			msg := fmt.Sprintf("No hosts found with port %d", port)
			if c.Query("exact") == "1" {
				msg = fmt.Sprintf("No hosts found for this service on port %d", port)
			}
			c.JSON(http.StatusNotFound, gin.H{"error": msg})
			return
		}

		sort.Slice(ips, func(i, j int) bool { return ipToSortKey(ips[i]) < ipToSortKey(ips[j]) })

		filename := fmt.Sprintf("hosts_port_%d.txt", port)
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(strings.Join(ips, "\n")+"\n"))
	}
}
