# Atlas

<p align="center">
  <img src="static/img/logo.png" alt="Atlas Logo">
</p>

Atlas is a self-hosted web application for managing penetration testing engagements. It consolidates scan data from common tools, tracks hosts and services, documents findings and credentials, and provides export functionality for reporting workflows. Built for consultants and internal security teams who want a single place to organize everything during an assessment.

---

## Table of Contents

- [Features](#features)
- [Getting Started](#getting-started)
  - [Requirements](#requirements)
  - [Docker](#docker)
  - [From Source](#from-source)
  - [Systemd Service](#systemd-service)
  - [Default Credentials](#default-credentials)
- [Usage](#usage)
  - [Projects](#projects)
  - [Dashboard](#dashboard)
  - [Hosts](#hosts)
  - [Host Detail](#host-detail)
  - [Services](#services)
  - [Findings](#findings)
  - [Finding Detail](#finding-detail)
  - [Credentials](#credentials)
  - [Discovered Users](#discovered-users)
  - [Uploads](#uploads)
  - [Exports](#exports)
  - [Settings](#settings)
- [File Parsing](#file-parsing)
  - [Supported Formats](#supported-formats)
  - [Atlas Raw JSON](#atlas-raw-json)
- [Administration](#administration)
  - [User Management](#user-management)
  - [Ngrok Tunnels](#ngrok-tunnels)
  - [Database Reset](#database-reset)
- [License](#license)

---

## Features

- **Project-based organization** - each engagement is its own isolated workspace with hosts, services, findings, credentials, and more
- **Scan file ingestion** - upload Nmap XML, Nessus, Nuclei, BBOT, HTTPX, Lair JSON, and Atlas Raw JSON files to auto-populate hosts and services
- **Host management** - color-coded status tracking, bulk operations, numeric IP sorting, and per-host detail pages with tabbed views for services, hostnames, credentials, web directories, and web probes
- **Findings tracker** - severity levels (critical through informational), CVSS scores, affected host mapping, CVE associations, and full description/remediation/evidence fields
- **Credential storage** - log discovered credentials with associated hosts and services, plus CSV export
- **Discovered users** - track usernames found during the engagement with single add, bulk add, and text file upload
- **Export generation** - PlexTrac assets, PlexTrac findings, Lair, and Atlas Raw JSON formats
- **Raw data portability** - export an entire project as a JSON file and import it into another Atlas instance, useful for handing off engagement data between consultants
- **Multi-user support** - per-project user assignments with owner/viewer roles
- **Built-in remote access** - optional ngrok tunnel integration for exposing Atlas without port forwarding
- **Dark mode UI** - purpose-built dark interface, no light mode

---

## Getting Started

### Requirements

- A modern web browser (Chrome, Firefox, Edge, Safari)

No external database server is required. Atlas uses an embedded SQLite database stored in your home directory at `~/.atlas/atlas.db`.

### Docker

The simplest way to run Atlas. Requires Docker and Docker Compose.

```bash
git clone https://github.com/waffl3ss/Atlas.git
cd Atlas
sudo docker compose up -d
```

Atlas will be available at **https://localhost** (port 443 on host, mapped to 8443 in the container). A self-signed TLS certificate is auto-generated on first run - your browser will show a certificate warning that you can accept.

**How data storage works:** The `docker-compose.yml` uses the `SUDO_USER` environment variable to bind-mount `/home/<your-user>/.atlas` into the container. When you run `sudo docker compose up`, the system automatically sets `SUDO_USER` to the user who invoked `sudo` (e.g. `kali`), so data is stored at `/home/kali/.atlas`. This works out of the box for the standard `sudo docker compose up` workflow.

**If you're not using sudo** (rootless Docker or running as root directly), `SUDO_USER` won't be set automatically. Edit the `volumes` line in `docker-compose.yml` to point to the correct home directory:

```yaml
volumes:
  # Default - works with sudo:
  - /home/${SUDO_USER}/.atlas:/home/atlas/.atlas

  # If running as root without sudo, change to:
  - /root/.atlas:/home/atlas/.atlas

  # If running rootless Docker as a specific user, change to:
  - /home/YOUR_USERNAME/.atlas:/home/atlas/.atlas
```

This is where Atlas stores its database, session key, and TLS certificates. The directory is created automatically on first run.

To use a different host port, edit the `ports` value in `docker-compose.yml`:

```yaml
ports:
  - "443:8443"   # change 443 to whatever you need
```

To use your own TLS certificates, place `cert.pem` and `key.pem` in `~/.atlas/` and add environment variables to `docker-compose.yml`:

```yaml
environment:
  - ATLAS_TLS_CERT=/home/atlas/.atlas/cert.pem
  - ATLAS_TLS_KEY=/home/atlas/.atlas/key.pem
```

```bash
sudo docker compose down       # stop the container
sudo docker compose logs -f    # view logs
```

### From Source

Requires Go 1.24 or later.

```bash
git clone https://github.com/waffl3ss/Atlas.git
cd Atlas
go mod download
```

Run directly:

```bash
go run cmd/atlas/main.go
```

Or build a standalone binary:

```bash
go build -o atlas ./cmd/atlas/
./atlas
```

Atlas serves HTTPS on port **8443** by default. Open your browser to `https://localhost:8443`. A self-signed TLS certificate is auto-generated on first run and stored in `~/.atlas/`. To use your own certificates, either place `cert.pem` and `key.pem` in `~/.atlas/` before starting Atlas, or point to them with environment variables:

```bash
ATLAS_TLS_CERT=/path/to/cert.pem ATLAS_TLS_KEY=/path/to/key.pem ./atlas
```

A Makefile is included for cross-compilation. Run `make help` to see all available targets including builds for Linux, macOS, and Windows on both amd64 and arm64.

### Systemd Service

For running Atlas as a system service from a compiled binary on Linux.

**1. Build the binary:**

```bash
make linux-amd64        # or linux-arm64 for ARM systems
```

**2. Set up the service user and directory:**

```bash
sudo useradd -r -m -d /home/atlas -s /usr/sbin/nologin atlas
sudo mkdir -p /opt/atlas
sudo cp atlas-linux-amd64 /opt/atlas/atlas
sudo cp -r templates/ /opt/atlas/templates/
sudo cp -r static/ /opt/atlas/static/
sudo chown -R atlas:atlas /opt/atlas
```

**3. Install the service file:**

```bash
sudo cp atlas.service /etc/systemd/system/
sudo systemctl daemon-reload
```

**4. Start and enable:**

```bash
sudo systemctl enable --now atlas
sudo systemctl status atlas
```

**5. View logs:**

```bash
journalctl -u atlas -f
```

Atlas will listen on HTTPS port 8443 with an auto-generated self-signed certificate. Use a reverse proxy (nginx, caddy) in front of it if you need a publicly trusted certificate, or place your own `cert.pem` and `key.pem` in `/home/atlas/.atlas/`. You can also set `ATLAS_TLS_CERT` and `ATLAS_TLS_KEY` environment variables in the service file to point to certificate files elsewhere on disk.

### Default Credentials

On first launch, Atlas creates an admin account:

- **Username:** admin
- **Password:** admin

> **Warning:** Change this password immediately after your first login through the admin user management page.

---

## Usage

### Projects

The projects page is the main landing screen after login. Each project represents a single engagement or assessment.

- Click **Create Project** to start a new engagement
- Projects accept a name (required), start/end dates (optional), and description (optional)
- Each project gets a unique 12-character ID displayed in the sidebar
- Users only see projects they are assigned to
- Projects are sorted newest-first

### Dashboard

The project dashboard provides a quick summary of what has been collected so far.

- Statistics cards show total counts for Hosts, Services, Findings, Credentials, and Discovered Users
- A pie chart breaks down findings by severity level
- The collapsible sidebar on the left provides navigation between all project pages

### Hosts

The hosts page lists every target system discovered or manually added for the engagement.

- **Color markers** - click the dot next to any host to cycle through grey, green, blue, yellow, orange, and red, useful for marking host status during testing
- **Filter bar** - text search by IP or hostname, plus clickable color dots to show/hide hosts by their color status
- **Add hosts** - the "Add Hosts" dropdown offers single add (one IP) or bulk add (paste a list of IPs, one per line)
- **Bulk operations** - select multiple hosts with checkboxes, then apply color changes or delete in bulk
- **Sorting** - IPs sort numerically, so 192.168.1.18 comes before 192.168.1.168

### Host Detail

Click any host IP to open its detail page with full tabbed information.

- **Services tab** - ports, protocols, service names, and banners for this host. Add, delete, bulk delete, and color-code individual services
- **Issues tab** - findings associated with this host, with severity and port/protocol info
- **Hostnames tab** - all hostnames associated with this host, with add and bulk delete
- **Credentials tab** - credentials linked to this host (matched by IP or hostname), with add and bulk delete
- **Web Directories tab** - discovered web paths on this host, organized by port
- **Web Stats tab** - HTTPX probe results including status codes (color-coded by range), titles, web server info, technologies, and redirect locations
- **Delete tab** - permanently remove the host and all associated data
- **Bottom info panel** - always visible at the bottom of the page, showing the host IP, MAC address, hostname, OS (editable), tag (editable), color, source, and last modified by
- **Navigation arrows** - move between hosts without going back to the list. The current tab is preserved when navigating

### Services

The services page provides an aggregated view of all services across the project.

- Services are grouped by port, protocol, service name, and version
- Click any row to open a slide-out panel listing all hosts running that service
- **Merge** - select multiple similar services and merge them into a single entry
- **Filter bar** - filter by port, protocol, service name, or banner text
- Bulk delete with checkbox selection

### Findings

The findings page tracks all security issues discovered during the engagement.

- Each finding has a severity level (critical, high, medium, low, informational), CVSS score, title, and associated host count
- **Color markers** - same color dot system as hosts for tracking finding status
- **Filter bar** - text search plus color filtering
- **Add Finding** - manual creation with title, severity, CVSS, synopsis, description, and solution fields
- Bulk delete and bulk color change via selection bar

### Finding Detail

Click any finding title to open the full detail view.

- **Description tab** - synopsis and detailed description, both editable
- **Remediation tab** - solution text, editable
- **Evidence tab** - supporting evidence or proof-of-concept details
- **CVEs tab** - associated CVE identifiers, add and remove individually or in bulk
- **Affected Hosts tab** - hosts impacted by this finding, with a searchable host selector that dynamically loads services from the selected host. Supports bulk add using `host:port/protocol` format, one entry per line
- **Delete tab** - permanently remove the finding
- **Bottom info panel** - severity badge, CVSS score, title (editable), source, color, and last modified by
- Navigation arrows to move between findings

### Credentials

The credentials page stores all discovered authentication data.

- Add credentials with username, password, host, service, type (e.g. SSH, HTTP, NTLM), and notes
- Bulk delete with checkbox selection
- **Export to CSV** - download all credentials as a CSV file

### Discovered Users

Track usernames found during testing that may not have associated passwords yet.

- **Add single user** - enter one username at a time
- **Bulk add** - paste a list of usernames separated by newlines
- **Upload from file** - import a .txt file with one username per line
- **Export** - download the full list as a .txt file
- Usernames are deduplicated per project

User lists can be created from tools like [SpiSuite](https://github.com/waffl3ss/SpiSuite) for general user enumeration and [GoKnock](https://github.com/waffl3ss/GoKnock) for user validation using Microsoft Teams and OneDrive, then imported directly into Atlas via bulk add or file upload.

### Uploads

The uploads page is where scan files are ingested into the project.

- **Drag and drop** or click to browse for files
- Atlas auto-detects the file type (Nmap, Nessus, Nuclei, BBOT, HTTPX, Lair, or Atlas Raw JSON)
- When uploading multiple files, Atlas processes them in priority order: host-creating tools first (Nmap, Nessus), then findings (Nuclei), then supplemental data (BBOT, HTTPX)
- Each upload shows the detected tool type, file size, who uploaded it, and when
- Delete individual uploads as needed

### Exports

Generate reports and data exports from the project.

- **PlexTrac Assets** - CSV format for importing hosts and services into PlexTrac
- **PlexTrac Findings** - CSV format for importing findings into PlexTrac
- **Lair** - JSON format compatible with the [Lair framework](https://github.com/lair-framework)
- **Generate Raw** - full project data export as Atlas-tagged JSON (see [Atlas Raw JSON](#atlas-raw-json) below)
- Download or delete previously generated exports

### Settings

Project configuration and team management.

- View and manage project members (add/remove users)
- Project owners can delete the project entirely

---

## File Parsing

### Supported Formats

| Format | Extension / Detection | What Gets Imported |
|--------|----------------------|-------------------|
| Nmap XML | `.xml` files with Nmap structure | Hosts (IP, hostname, MAC, OS) and services (port, protocol, name, version) |
| Nessus | `.nessus` files | Hosts, services, and findings with severity, description, solution, CVEs, and affected host mapping |
| Nuclei | Files containing Nuclei JSON output | Findings mapped to existing hosts |
| BBOT | Files containing BBOT JSON output | Enrichment data for existing hosts |
| HTTPX | Files containing HTTPX JSONL output | Web probe data (status codes, titles, servers, technologies) for existing hosts |
| Lair JSON | JSON files with `longIpv4Addr` marker | Hosts, services, findings, and credentials from Lair framework exports |
| Atlas Raw JSON | JSON files with `_atlas_export` marker | Full project data (see below) |

**Note:** BBOT and HTTPX are supplemental parsers. They only enrich hosts that already exist in the project. Upload Nmap or Nessus files first to create the host entries, then upload HTTPX/BBOT data to add web probe and enrichment information.

### Atlas Raw JSON

The Atlas Raw JSON format is designed for portability between Atlas instances. When you generate a raw export, Atlas packages up everything in the project:

- All hosts with their services, hostnames, web directories, and web probes
- All findings with descriptions, remediation, evidence, CVEs, and affected host mappings
- All credentials
- All discovered users
- Project metadata and a timestamp

The resulting JSON file is tagged with `_atlas_export: true` so Atlas can recognize it on upload. Host and finding deduplication is handled during import, so uploading the same raw file twice will not create duplicate entries.

Use this to hand off test data to another tester if needed.

---

## Administration

Admin features are accessible from the user dropdown menu in the header (visible only to admin accounts).

### User Management

- Create new user accounts
- Delete existing accounts
- Change passwords for any user
- Only admin users can access this page

### Ngrok Tunnels

Atlas has built-in ngrok integration for exposing the application over the internet without manual port forwarding.

- Save your ngrok auth token (Auth token, not API key...)
- Start and stop tunnels from the web interface
- The public URL is displayed when a tunnel is active
- A status indicator in the header shows when ngrok is running
- The tunnel serves traffic directly through the Atlas handler - no separate port or backend forwarding is involved

### Database Reset

Available through the admin dropdown, the database reset option wipes all data and returns Atlas to a fresh state. This requires a double confirmation to prevent accidental use.

---

## License

This project is licensed under the GNU General Public License v3.0. See [LICENSE](LICENSE) for the full text.
