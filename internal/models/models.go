package models

import (
	"crypto/rand"
	"encoding/base64"
	"time"
)

type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	StartDate   string    `json:"start_date"`
	EndDate     string    `json:"end_date"`
	CreatorID   int       `json:"creator_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProjectUser struct {
	ID        int       `json:"id"`
	ProjectID string    `json:"project_id"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Host struct {
	ID             int       `json:"id"`
	ProjectID      string    `json:"project_id"`
	IPAddress      string    `json:"ip_address"`
	Hostname       string    `json:"hostname"`
	OS             string    `json:"os"`
	Notes          string    `json:"notes"`
	Color          string    `json:"color"`
	Source         string    `json:"source"`
	MACAddress     string    `json:"mac_address"`
	Tag            string    `json:"tag"`
	LastModifiedBy *int      `json:"last_modified_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type HostWithUser struct {
	Host
	ModifiedByUsername string `json:"modified_by_username"`
}

type Service struct {
	ID          int       `json:"id"`
	HostID      int       `json:"host_id"`
	ProjectID   string    `json:"project_id"`
	Port        int       `json:"port"`
	Protocol    string    `json:"protocol"`
	ServiceName string    `json:"service_name"`
	Version     string    `json:"version"`
	Notes       string    `json:"notes"`
	Color       string    `json:"color"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
}

type Finding struct {
	ID             int       `json:"id"`
	ProjectID      string    `json:"project_id"`
	Title          string    `json:"title"`
	Severity       string    `json:"severity"`
	CvssScore      float64   `json:"cvss_score"`
	CvssVector     string    `json:"cvss_vector"`
	Description    string    `json:"description"`
	Synopsis       string    `json:"synopsis"`
	Solution       string    `json:"solution"`
	Evidence       string    `json:"evidence"`
	PluginID       string    `json:"plugin_id"`
	PluginSource   string    `json:"plugin_source"`
	Color          string    `json:"color"`
	Source         string    `json:"source"`
	LastModifiedBy *int      `json:"last_modified_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type FindingWithUser struct {
	Finding
	ModifiedByUsername string `json:"modified_by_username"`
	HostCount          int    `json:"host_count"`
}

type FindingHost struct {
	ID           int    `json:"id"`
	FindingID    int    `json:"finding_id"`
	HostID       int    `json:"host_id"`
	Port         int    `json:"port"`
	Protocol     string `json:"protocol"`
	PluginOutput string `json:"plugin_output"`
	IPAddress    string `json:"ip_address"`
	Hostname     string `json:"hostname"`
}

type FindingCVE struct {
	ID        int    `json:"id"`
	FindingID int    `json:"finding_id"`
	CVE       string `json:"cve"`
}

type HostFinding struct {
	FindingHostID int     `json:"finding_host_id"`
	FindingID     int     `json:"finding_id"`
	Title         string  `json:"title"`
	Severity      string  `json:"severity"`
	CvssScore     float64 `json:"cvss_score"`
	Port          int     `json:"port"`
	Protocol      string  `json:"protocol"`
	Source        string  `json:"source"`
}

type Credential struct {
	ID             int       `json:"id"`
	ProjectID      string    `json:"project_id"`
	HostID         *int      `json:"host_id"`
	ServiceID      *int      `json:"service_id"`
	Username       string    `json:"username"`
	Password       string    `json:"password"`
	CredentialType string    `json:"credential_type"`
	Host           string    `json:"host"`
	Service        string    `json:"service"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
}

type DashboardStats struct {
	TotalHosts       int                 `json:"total_hosts"`
	TotalServices    int                 `json:"total_services"`
	TotalFindings    int                 `json:"total_findings"`
	FindingsBySev    map[string]int      `json:"findings_by_severity"`
	TotalUsers       int                 `json:"total_users"`
	TotalCredentials int                 `json:"total_credentials"`
}

type Upload struct {
	ID         int       `json:"id"`
	ProjectID  string    `json:"project_id"`
	Filename   string    `json:"filename"`
	StoredPath string    `json:"stored_path"`
	FileSize   int64     `json:"file_size"`
	ToolType   string    `json:"tool_type"`
	UploadedBy int       `json:"uploaded_by"`
	CreatedAt  time.Time `json:"created_at"`
}

type UploadWithUser struct {
	Upload
	Username string `json:"username"`
}

type Export struct {
	ID          int       `json:"id"`
	ProjectID   string    `json:"project_id"`
	Filename    string    `json:"filename"`
	StoredPath  string    `json:"stored_path"`
	ExportType  string    `json:"export_type"`
	GeneratedBy int       `json:"generated_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type ExportWithUser struct {
	Export
	Username string `json:"username"`
}

type DiscoveredUser struct {
	ID        int       `json:"id"`
	ProjectID string    `json:"project_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// HostnameEntry represents a hostname associated with a host
type HostnameEntry struct {
	ID        int    `json:"id"`
	HostID    int    `json:"host_id"`
	ProjectID string `json:"project_id"`
	Hostname  string `json:"hostname"`
	Source    string `json:"source"`
}

// WebDirectory represents a web directory/path discovered on a host
type WebDirectory struct {
	ID         int    `json:"id"`
	HostID     int    `json:"host_id"`
	ProjectID  string `json:"project_id"`
	Port       int    `json:"port"`
	BaseDomain string `json:"base_domain"`
	Path       string `json:"path"`
	Source     string `json:"source"`
}

// WebProbe represents an HTTPX probe result for a web service on a host
type WebProbe struct {
	ID            int    `json:"id"`
	HostID        int    `json:"host_id"`
	ProjectID     string `json:"project_id"`
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
	Source        string `json:"source"`
}

// AggregatedService represents a unique service (port+protocol+name+banner) with host count
type AggregatedService struct {
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	ServiceName string `json:"service_name"`
	Banner      string `json:"banner"`
	HostCount   int    `json:"host_count"`
	HostIPs     string `json:"host_ips"`
}

// ServiceHost represents a host associated with a service
type ServiceHost struct {
	HostID    int    `json:"host_id"`
	IPAddress string `json:"ip_address"`
	Hostname  string `json:"hostname"`
	OS        string `json:"os"`
}

// GenerateProjectID generates a random 12-character ID
func GenerateProjectID() (string, error) {
	bytes := make([]byte, 9) // 9 bytes = 12 base64 characters
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes)[:12], nil
}
