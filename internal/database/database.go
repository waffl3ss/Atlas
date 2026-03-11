package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

// Initialize creates the database in the user's home directory and sets up tables
func Initialize() (*sql.DB, error) {
	dbPath, err := getDBPath()
	if err != nil {
		return nil, err
	}

	// Ensure the directory exists
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Force a single connection so PRAGMAs apply to all queries
	db.SetMaxOpenConns(1)

	// Enable foreign key constraints (required for ON DELETE CASCADE)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create tables
	if err := createTables(db); err != nil {
		db.Close()
		return nil, err
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, err
	}

	// Create default admin user if no users exist
	if err := createDefaultUser(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

// GetDBPath returns the database path (exposed for reset functionality)
func GetDBPath() (string, error) {
	return getDBPath()
}

// getDBPath returns the appropriate database path for the OS
func getDBPath() (string, error) {
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

	// Use .atlas directory (hidden on Unix-like systems)
	atlasDir := filepath.Join(homeDir, ".atlas")
	dbPath := filepath.Join(atlasDir, "atlas.db")

	return dbPath, nil
}

// runMigrations runs database migrations
func runMigrations(db *sql.DB) error {
	// Create migrations table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Migration 1: Add creator_id to projects table if it doesn't exist
	var migrationApplied bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_creator_id_to_projects')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		// Check if creator_id column exists
		var columnExists bool
		err = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name='creator_id'").Scan(&columnExists)
		if err == nil && !columnExists {
			// Add creator_id column with default value 1 (admin)
			_, err = db.Exec("ALTER TABLE projects ADD COLUMN creator_id INTEGER NOT NULL DEFAULT 1")
			if err != nil {
				return fmt.Errorf("failed to add creator_id column: %w", err)
			}
			fmt.Println("Migration applied: Added creator_id to projects table")
		}

		// Mark migration as applied
		_, err = db.Exec("INSERT INTO migrations (name) VALUES ('add_creator_id_to_projects')")
		if err != nil {
			return fmt.Errorf("failed to record migration: %w", err)
		}
	}

	// Migration 2: Add host and service text columns to credentials table
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_host_service_text_to_credentials')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		var hostColExists bool
		db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('credentials') WHERE name='host'").Scan(&hostColExists)
		if !hostColExists {
			if _, err := db.Exec("ALTER TABLE credentials ADD COLUMN host TEXT DEFAULT ''"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			if _, err := db.Exec("ALTER TABLE credentials ADD COLUMN service TEXT DEFAULT ''"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			fmt.Println("Migration applied: Added host and service text columns to credentials table")
		}

		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_host_service_text_to_credentials')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 3: Add color, source, last_modified_by, updated_at to hosts table
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_host_tracking_columns')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		var colorColExists bool
		db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('hosts') WHERE name='color'").Scan(&colorColExists)
		if !colorColExists {
			if _, err := db.Exec("ALTER TABLE hosts ADD COLUMN color TEXT DEFAULT 'grey'"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			if _, err := db.Exec("ALTER TABLE hosts ADD COLUMN source TEXT DEFAULT 'manual'"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			if _, err := db.Exec("ALTER TABLE hosts ADD COLUMN last_modified_by INTEGER REFERENCES users(id)"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			if _, err := db.Exec("ALTER TABLE hosts ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			fmt.Println("Migration applied: Added color, source, last_modified_by, updated_at to hosts table")
		}

		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_host_tracking_columns')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 4: Add mac_address and tag to hosts table
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_host_mac_tag')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		var macColExists bool
		db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('hosts') WHERE name='mac_address'").Scan(&macColExists)
		if !macColExists {
			if _, err := db.Exec("ALTER TABLE hosts ADD COLUMN mac_address TEXT DEFAULT ''"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			if _, err := db.Exec("ALTER TABLE hosts ADD COLUMN tag TEXT DEFAULT ''"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			fmt.Println("Migration applied: Added mac_address and tag to hosts table")
		}

		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_host_mac_tag')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 5: Add color to services table
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_service_color')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		var colorColExists bool
		db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('services') WHERE name='color'").Scan(&colorColExists)
		if !colorColExists {
			if _, err := db.Exec("ALTER TABLE services ADD COLUMN color TEXT DEFAULT 'grey'"); err != nil {
				fmt.Printf("Warning: %v\n", err)
			}
			fmt.Println("Migration applied: Added color to services table")
		}

		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_service_color')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 6: Create hostnames table and migrate existing hostname data
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'create_hostnames_table')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS hostnames (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host_id INTEGER NOT NULL,
			project_id TEXT NOT NULL,
			hostname TEXT NOT NULL,
			source TEXT DEFAULT 'manual',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(host_id, hostname)
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hostnames_host ON hostnames(host_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		// Migrate existing hostname data from hosts table
		if _, err := db.Exec(`INSERT OR IGNORE INTO hostnames (host_id, project_id, hostname, source)
			SELECT id, project_id, hostname, 'nmap' FROM hosts
			WHERE hostname IS NOT NULL AND hostname != ''`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Created hostnames table and migrated existing data")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('create_hostnames_table')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 7: Create web_directories table
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'create_web_directories')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS web_directories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host_id INTEGER NOT NULL,
			project_id TEXT NOT NULL,
			port INTEGER NOT NULL,
			base_domain TEXT DEFAULT '',
			path TEXT NOT NULL,
			source TEXT DEFAULT 'manual',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(host_id, port, base_domain, path)
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_web_directories_host ON web_directories(host_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Created web_directories table")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('create_web_directories')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 8: Add base_domain column to web_directories and fix UNIQUE constraint
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_base_domain_to_web_directories')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		// SQLite requires table recreation to change UNIQUE constraints
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS web_directories_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host_id INTEGER NOT NULL,
			project_id TEXT NOT NULL,
			port INTEGER NOT NULL,
			base_domain TEXT DEFAULT '',
			path TEXT NOT NULL,
			source TEXT DEFAULT 'manual',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(host_id, port, base_domain, path)
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`INSERT INTO web_directories_new (id, host_id, project_id, port, base_domain, path, source, created_at)
			SELECT id, host_id, project_id, port, '', path, source, created_at FROM web_directories`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`DROP TABLE web_directories`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE web_directories_new RENAME TO web_directories`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_web_directories_host ON web_directories(host_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Added base_domain to web_directories")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_base_domain_to_web_directories')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 9: Create web_probes table
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'create_web_probes')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS web_probes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			host_id INTEGER NOT NULL,
			project_id TEXT NOT NULL,
			port INTEGER NOT NULL,
			scheme TEXT DEFAULT '',
			url TEXT DEFAULT '',
			title TEXT DEFAULT '',
			status_code INTEGER DEFAULT 0,
			webserver TEXT DEFAULT '',
			content_type TEXT DEFAULT '',
			content_length INTEGER DEFAULT 0,
			tech TEXT DEFAULT '',
			location TEXT DEFAULT '',
			response_time TEXT DEFAULT '',
			source TEXT DEFAULT 'manual',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(host_id, port, scheme)
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_web_probes_host ON web_probes(host_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Created web_probes table")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('create_web_probes')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 10: Add source column to services table
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_source_to_services')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		if _, err := db.Exec(`ALTER TABLE services ADD COLUMN source TEXT DEFAULT 'manual'`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		fmt.Println("Migration applied: Added source column to services")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_source_to_services')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 11: Update uploads table to allow httpx tool_type
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_httpx_tool_type')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		// Recreate uploads table with updated CHECK constraint
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS uploads_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			filename TEXT NOT NULL,
			stored_path TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			tool_type TEXT NOT NULL CHECK(tool_type IN ('nmap', 'nessus', 'nuclei', 'bbot', 'httpx', 'atlas_raw')),
			uploaded_by INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE CASCADE
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`INSERT INTO uploads_new SELECT * FROM uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`DROP TABLE uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE uploads_new RENAME TO uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_uploads_project ON uploads(project_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Added httpx to uploads tool_type")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_httpx_tool_type')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 12: Rebuild findings table with expanded schema, create finding_hosts and finding_cves
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'rebuild_findings_v2')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		// Create new findings table
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS findings_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			severity TEXT NOT NULL CHECK(severity IN ('critical', 'high', 'medium', 'low', 'informational')),
			cvss_score REAL DEFAULT 0,
			cvss_vector TEXT DEFAULT '',
			description TEXT DEFAULT '',
			synopsis TEXT DEFAULT '',
			solution TEXT DEFAULT '',
			evidence TEXT DEFAULT '',
			plugin_id TEXT DEFAULT '',
			plugin_source TEXT DEFAULT '',
			color TEXT DEFAULT 'grey',
			source TEXT DEFAULT 'manual',
			last_modified_by INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (last_modified_by) REFERENCES users(id)
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		// Migrate existing findings data (preserve title, severity, description)
		if _, err := db.Exec(`INSERT INTO findings_new (id, project_id, title, severity, description, created_at, updated_at)
			SELECT id, project_id, title, severity, COALESCE(description, ''), created_at, updated_at
			FROM findings`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		if _, err := db.Exec(`DROP TABLE IF EXISTS findings`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE findings_new RENAME TO findings`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_findings_project ON findings(project_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		// Create finding_hosts junction table
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS finding_hosts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			finding_id INTEGER NOT NULL,
			host_id INTEGER NOT NULL,
			port INTEGER DEFAULT 0,
			protocol TEXT DEFAULT '',
			plugin_output TEXT DEFAULT '',
			FOREIGN KEY (finding_id) REFERENCES findings(id) ON DELETE CASCADE,
			FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
			UNIQUE(finding_id, host_id, port)
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_finding_hosts_finding ON finding_hosts(finding_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_finding_hosts_host ON finding_hosts(host_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		// Create finding_cves table
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS finding_cves (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			finding_id INTEGER NOT NULL,
			cve TEXT NOT NULL,
			FOREIGN KEY (finding_id) REFERENCES findings(id) ON DELETE CASCADE,
			UNIQUE(finding_id, cve)
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_finding_cves_finding ON finding_cves(finding_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Rebuilt findings table with expanded schema, created finding_hosts and finding_cves")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('rebuild_findings_v2')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 13: Add atlas_raw to exports and uploads CHECK constraints
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_atlas_raw_type')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		// Recreate exports table with updated CHECK constraint
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS exports_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			filename TEXT NOT NULL,
			stored_path TEXT NOT NULL,
			export_type TEXT NOT NULL CHECK(export_type IN ('plextrac_assets', 'plextrac_findings', 'lair', 'atlas_raw')),
			generated_by INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (generated_by) REFERENCES users(id) ON DELETE CASCADE
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`INSERT INTO exports_new SELECT * FROM exports`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`DROP TABLE exports`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE exports_new RENAME TO exports`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_exports_project ON exports(project_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		// Recreate uploads table with updated CHECK constraint
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS uploads_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			filename TEXT NOT NULL,
			stored_path TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			tool_type TEXT NOT NULL CHECK(tool_type IN ('nmap', 'nessus', 'nuclei', 'bbot', 'httpx', 'atlas_raw')),
			uploaded_by INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE CASCADE
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`INSERT INTO uploads_new SELECT * FROM uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`DROP TABLE uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE uploads_new RENAME TO uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_uploads_project ON uploads(project_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Added atlas_raw to exports and uploads type constraints")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_atlas_raw_type')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration: Create export_tags table
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'create_export_tags')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS export_tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			name TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			UNIQUE(project_id, name)
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_export_tags_project ON export_tags(project_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Created export_tags table")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('create_export_tags')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 14: Add lair to uploads tool_type CHECK constraint
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'add_lair_tool_type')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS uploads_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			filename TEXT NOT NULL,
			stored_path TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			tool_type TEXT NOT NULL CHECK(tool_type IN ('nmap', 'nessus', 'nuclei', 'bbot', 'httpx', 'atlas_raw', 'lair')),
			uploaded_by INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE CASCADE
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`INSERT INTO uploads_new SELECT * FROM uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`DROP TABLE uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE uploads_new RENAME TO uploads`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_uploads_project ON uploads(project_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Added lair to uploads tool_type")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('add_lair_tool_type')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	// Migration 15: Fix foreign key constraints for user deletion (ON DELETE SET NULL)
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM migrations WHERE name = 'fix_user_deletion_fk')").Scan(&migrationApplied)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if !migrationApplied {
		// Recreate hosts table with ON DELETE SET NULL for last_modified_by
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS hosts_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			ip_address TEXT NOT NULL,
			hostname TEXT,
			os TEXT,
			notes TEXT,
			color TEXT DEFAULT 'grey',
			source TEXT DEFAULT 'manual',
			mac_address TEXT DEFAULT '',
			tag TEXT DEFAULT '',
			last_modified_by INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (last_modified_by) REFERENCES users(id) ON DELETE SET NULL
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`INSERT INTO hosts_new SELECT * FROM hosts`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`DROP TABLE hosts`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE hosts_new RENAME TO hosts`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_hosts_project ON hosts(project_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		// Recreate findings table with ON DELETE SET NULL for last_modified_by
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS findings_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			severity TEXT NOT NULL CHECK(severity IN ('critical', 'high', 'medium', 'low', 'informational')),
			cvss_score REAL DEFAULT 0,
			cvss_vector TEXT DEFAULT '',
			description TEXT DEFAULT '',
			synopsis TEXT DEFAULT '',
			solution TEXT DEFAULT '',
			evidence TEXT DEFAULT '',
			plugin_id TEXT DEFAULT '',
			plugin_source TEXT DEFAULT '',
			color TEXT DEFAULT 'grey',
			source TEXT DEFAULT 'manual',
			last_modified_by INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
			FOREIGN KEY (last_modified_by) REFERENCES users(id) ON DELETE SET NULL
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`INSERT INTO findings_new SELECT * FROM findings`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`DROP TABLE findings`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE findings_new RENAME TO findings`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_findings_project ON findings(project_id)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		// Recreate projects table with ON DELETE SET NULL for creator_id (allow NULL)
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS projects_new (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			start_date TEXT,
			end_date TEXT,
			creator_id INTEGER DEFAULT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (creator_id) REFERENCES users(id) ON DELETE SET NULL
		)`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`INSERT INTO projects_new SELECT * FROM projects`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`DROP TABLE projects`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
		if _, err := db.Exec(`ALTER TABLE projects_new RENAME TO projects`); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}

		fmt.Println("Migration applied: Fixed foreign key constraints for user deletion")
		if _, err := db.Exec("INSERT INTO migrations (name) VALUES ('fix_user_deletion_fk')"); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	return nil
}

// createTables creates all necessary database tables
func createTables(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		start_date TEXT,
		end_date TEXT,
		creator_id INTEGER DEFAULT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (creator_id) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS hosts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		ip_address TEXT NOT NULL,
		hostname TEXT,
		os TEXT,
		notes TEXT,
		color TEXT DEFAULT 'grey',
		source TEXT DEFAULT 'manual',
		mac_address TEXT DEFAULT '',
		tag TEXT DEFAULT '',
		last_modified_by INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		FOREIGN KEY (last_modified_by) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS services (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL,
		project_id TEXT NOT NULL,
		port INTEGER NOT NULL,
		protocol TEXT,
		service_name TEXT,
		version TEXT,
		notes TEXT,
		color TEXT DEFAULT 'grey',
		source TEXT DEFAULT 'manual',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS findings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		title TEXT NOT NULL,
		severity TEXT NOT NULL CHECK(severity IN ('critical', 'high', 'medium', 'low', 'informational')),
		cvss_score REAL DEFAULT 0,
		cvss_vector TEXT DEFAULT '',
		description TEXT DEFAULT '',
		synopsis TEXT DEFAULT '',
		solution TEXT DEFAULT '',
		evidence TEXT DEFAULT '',
		plugin_id TEXT DEFAULT '',
		plugin_source TEXT DEFAULT '',
		color TEXT DEFAULT 'grey',
		source TEXT DEFAULT 'manual',
		last_modified_by INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		FOREIGN KEY (last_modified_by) REFERENCES users(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS finding_hosts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		finding_id INTEGER NOT NULL,
		host_id INTEGER NOT NULL,
		port INTEGER DEFAULT 0,
		protocol TEXT DEFAULT '',
		plugin_output TEXT DEFAULT '',
		FOREIGN KEY (finding_id) REFERENCES findings(id) ON DELETE CASCADE,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
		UNIQUE(finding_id, host_id, port)
	);

	CREATE TABLE IF NOT EXISTS finding_cves (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		finding_id INTEGER NOT NULL,
		cve TEXT NOT NULL,
		FOREIGN KEY (finding_id) REFERENCES findings(id) ON DELETE CASCADE,
		UNIQUE(finding_id, cve)
	);

	CREATE TABLE IF NOT EXISTS credentials (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		host_id INTEGER,
		service_id INTEGER,
		username TEXT,
		password TEXT,
		credential_type TEXT,
		host TEXT DEFAULT '',
		service TEXT DEFAULT '',
		notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE SET NULL,
		FOREIGN KEY (service_id) REFERENCES services(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS project_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		user_id INTEGER NOT NULL,
		role TEXT DEFAULT 'viewer',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
		UNIQUE(project_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS uploads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		filename TEXT NOT NULL,
		stored_path TEXT NOT NULL,
		file_size INTEGER NOT NULL,
		tool_type TEXT NOT NULL CHECK(tool_type IN ('nmap', 'nessus', 'nuclei', 'bbot', 'httpx', 'atlas_raw', 'lair')),
		uploaded_by INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS discovered_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		username TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		UNIQUE(project_id, username)
	);

	CREATE TABLE IF NOT EXISTS exports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		filename TEXT NOT NULL,
		stored_path TEXT NOT NULL,
		export_type TEXT NOT NULL CHECK(export_type IN ('plextrac_assets', 'plextrac_findings', 'lair', 'atlas_raw')),
		generated_by INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		FOREIGN KEY (generated_by) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS hostnames (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL,
		project_id TEXT NOT NULL,
		hostname TEXT NOT NULL,
		source TEXT DEFAULT 'manual',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		UNIQUE(host_id, hostname)
	);

	CREATE INDEX IF NOT EXISTS idx_hosts_project ON hosts(project_id);
	CREATE INDEX IF NOT EXISTS idx_services_project ON services(project_id);
	CREATE INDEX IF NOT EXISTS idx_services_host ON services(host_id);
	CREATE INDEX IF NOT EXISTS idx_findings_project ON findings(project_id);
	CREATE INDEX IF NOT EXISTS idx_finding_hosts_finding ON finding_hosts(finding_id);
	CREATE INDEX IF NOT EXISTS idx_finding_hosts_host ON finding_hosts(host_id);
	CREATE INDEX IF NOT EXISTS idx_finding_cves_finding ON finding_cves(finding_id);
	CREATE INDEX IF NOT EXISTS idx_credentials_project ON credentials(project_id);
	CREATE INDEX IF NOT EXISTS idx_uploads_project ON uploads(project_id);
	CREATE INDEX IF NOT EXISTS idx_discovered_users_project ON discovered_users(project_id);
	CREATE INDEX IF NOT EXISTS idx_hostnames_host ON hostnames(host_id);

	CREATE TABLE IF NOT EXISTS web_directories (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL,
		project_id TEXT NOT NULL,
		port INTEGER NOT NULL,
		base_domain TEXT DEFAULT '',
		path TEXT NOT NULL,
		source TEXT DEFAULT 'manual',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		UNIQUE(host_id, port, base_domain, path)
	);

	CREATE INDEX IF NOT EXISTS idx_web_directories_host ON web_directories(host_id);

	CREATE TABLE IF NOT EXISTS web_probes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		host_id INTEGER NOT NULL,
		project_id TEXT NOT NULL,
		port INTEGER NOT NULL,
		scheme TEXT DEFAULT '',
		url TEXT DEFAULT '',
		title TEXT DEFAULT '',
		status_code INTEGER DEFAULT 0,
		webserver TEXT DEFAULT '',
		content_type TEXT DEFAULT '',
		content_length INTEGER DEFAULT 0,
		tech TEXT DEFAULT '',
		location TEXT DEFAULT '',
		response_time TEXT DEFAULT '',
		source TEXT DEFAULT 'manual',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (host_id) REFERENCES hosts(id) ON DELETE CASCADE,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		UNIQUE(host_id, port, scheme)
	);

	CREATE INDEX IF NOT EXISTS idx_web_probes_host ON web_probes(host_id);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_exports_project ON exports(project_id);

	CREATE TABLE IF NOT EXISTS export_tags (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL,
		name TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
		UNIQUE(project_id, name)
	);

	CREATE INDEX IF NOT EXISTS idx_export_tags_project ON export_tags(project_id);
	`

	_, err := db.Exec(schema)
	return err
}

// createDefaultUser creates a default admin user if no users exist
func createDefaultUser(db *sql.DB) error {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return err
	}

	if count == 0 {
		// Create default admin user with password "admin"
		hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		_, err = db.Exec("INSERT INTO users (username, password_hash) VALUES (?, ?)", "admin", string(hash))
		if err != nil {
			return err
		}

		fmt.Println("Default admin user created (username: admin, password: admin)")
		fmt.Println("Please change the password after first login!")
	}

	return nil
}

// ResetSchema drops all tables and recreates them (for database reset)
func ResetSchema(db *sql.DB) error {
	// Drop all tables in reverse order of dependencies
	tables := []string{
		"settings",
		"migrations",
		"web_probes",
		"web_directories",
		"hostnames",
		"export_tags",
		"exports",
		"discovered_users",
		"uploads",
		"project_users",
		"credentials",
		"finding_cves",
		"finding_hosts",
		"findings",
		"services",
		"hosts",
		"projects",
		"users",
	}

	for _, table := range tables {
		_, err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", table))
		if err != nil {
			return fmt.Errorf("failed to drop table %s: %w", table, err)
		}
	}

	// Recreate all tables
	if err := createTables(db); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create default admin user
	if err := createDefaultUser(db); err != nil {
		return fmt.Errorf("failed to create default user: %w", err)
	}

	fmt.Println("Database schema reset successfully")
	return nil
}
