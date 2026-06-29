# WP Panel

A dedicated server management panel for WordPress. One line of code turns a clean Debian 13 into a WordPress hosting platform.

[![License](https://img.shields.io/badge/license-GPL--3.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](https://go.dev/)

---

## Official Sources

- Official website: <https://wp-panel.org>
- GitHub repository: <https://github.com/CalvinSmall/wp-panel-en-private>

Aside from `wp-panel.org` and this GitHub repository, no other domain is an official WP Panel website or affiliated with this project.

---

## What It Is

General-purpose Linux panels are bloated, overly complex, and packed with features irrelevant to WordPress.

WP Panel does one thing: **efficiently manage WordPress sites on a VPS**. No Docker, no mail systems, no FTP, no Java/Python/Node runtimes.

## Feature Modules

| Module | Description |
|------|------|
| **Site Management** | One-click site creation (auto-creates isolated user/directory/Nginx/PHP-FPM/database), suspend/enable/delete, reinstall WordPress |
| **SSL Certificates** | Let's Encrypt auto-issuance, auto-renewal 30 days before expiry, manual replacement, self-signed certificates |
| **FastCGI Cache** | Nginx full-page static cache, with companion WordPress plugin for one-click cache clearing |
| **Security Defense** | Fail2ban + nftables dual-engine progressive banning, Cloudflare/Google/Bing official whitelists, global rate limiting |
| **Database Management** | MariaDB password changes, database backup/restore/upload restore/auto-backup |
| **Scheduled Tasks** | Visual Cron management, WP Cron replacement, incremental file backup, system task viewer |
| **File Manager** | Upload/download/delete/rename/compress/extract/cut/copy/paste/multi-select, chunked upload + resume |
| **Dashboard** | Real-time CPU/memory/disk/load monitoring, 24h/7d/15d historical trend charts |
| **Alert Notifications** | SMTP email alerts with independent toggles for CPU/memory/disk/service/SSL/site expiry/system update/panel update |
| **Software Management** | PHP/Nginx/MariaDB/Redis config editing, process guardian, log viewer |
| **Panel Security** | Randomized entry path + BasicAuth + Web dual authentication, bcrypt password hashing, login failure banning |
| **Version Updates** | In-panel one-click update check, SHA256 + Ed25519 dual verification, automatic rollback on failure, configurable proxy for China |

## One-Click Install

```bash
apt-get update && apt-get install -y wget ca-certificates && wget -qO- https://raw.githubusercontent.com/CalvinSmall/wp-panel-en-private/main/install.sh | bash
```

**Servers in China**: If GitHub is unreachable, use the China-optimized script:

```bash
apt-get update && apt-get install -y wget ca-certificates && wget -qO- https://raw.githubusercontent.com/CalvinSmall/wp-panel-en-private/main/install.sh | bash
```

After installation, the panel URL and two-layer login credentials (BasicAuth + Web login) will be displayed.

> Since a self-signed certificate is used, browsers will show a security warning on first visit. Click "Advanced" → "Proceed to site" to continue.

## Security

**In one sentence: as long as the login URL and credentials are not leaked, no one else can get in.**

The panel has four layers of defense:
- Layer 0: **Scan Defense** — Non-browser requests hitting port 8443 are immediately identified and blocked at the nftables network layer for 30 days
- Layer 1: Random Entry — 8-character random hex path (16^8 ≈ 4.3 billion combinations); scanners cannot guess it
- Layer 2: BasicAuth — Browser prompt requiring username and password
- Layer 3: Web Login — Web form requiring panel login password

Only after passing all four layers can anyone access the panel. Five failures at any layer result in an automatic ban.

---

More detailed security mechanisms:

**Access Protection**
- **Scan Defense**: Panel port 8443 auto-detects non-browser requests (curl, scripts, scanners) and blocks them at the nftables network layer
- Random entry path (16^8 ≈ 4.3 billion combinations; brute-forcing impossible when combined with scan defense)
- BasicAuth + Web login dual authentication
- Pure HTTPS encrypted communication; the panel only exposes port 8443 to the public
- API error messages do not leak internal paths or command output

**Anti-Brute-Force**
- 5 consecutive failures at any authentication layer → nftables network-layer ban for 24 hours
- Multi-level progressive banning: 10 minutes → 24 hours → 30 days → permanent

**Site Isolation**
- Each site runs under its own system user and PHP-FPM Pool
- Each site uses its own MariaDB database
- Issues with one site do not affect others

**WordPress-Specific Protection**
- Auto-detects and blocks wp-login.php brute-force attempts and xmlrpc.php malicious requests
- Sensitive file scan detection (.env, .git, archives, etc.) → auto-ban
- 404 flood detection: 30 requests/60 seconds triggers directory scan ban
- Nginx rejects HTTPS connections from unknown hostnames, preventing certificate info leaks
- Logged-in WordPress users are automatically exempt from rate limiting, so admin operations are unaffected

**Update Security**
- Panel updates use SHA256 + Ed25519 dual verification; attackers cannot forge signatures even if GitHub Releases are compromised
- Failed updates automatically roll back to the previous version, so the panel continues to operate normally

**Code Transparency**
- 100% open source (GPL-3.0); code is auditable
- No sensitive business data is collected; anonymous stats (version number only) can be turned off with one click in the panel
- Update checks only connect to GitHub; no other external services are contacted
- No web shell, no online code editing
- Passwords stored as bcrypt hash (cost 12); no plaintext storage
- Three rounds of AI security auditing have resolved 44 potential issues

### 📖 In-Depth Security Analysis

- **[Install Script Security Transparency Report](security/wp-panel-install-security.md)** — A line-by-line breakdown of install.sh, addressing claims of "tampering with passwords, deleting Nginx, hacking WordPress"
- **[Runtime Security: Multi-Layer Defense Mechanisms](security/wp-panel-runtime-security.md)** — Source-level analysis of six-layer defense-in-depth, update signature verification, and software vulnerability management

## Security Testing

White-hat researchers and security professionals are welcome to test this project. If you discover a vulnerability, please report it via:

- **Public report**: Submit a [GitHub Issue](https://github.com/CalvinSmall/wp-panel-en-private/issues) with `[Security]` in the title
- **Private report**: Submit a Private Vulnerability Report via the GitHub Security tab
- Valid vulnerabilities will be acknowledged with credit to the reporter in the Release Notes after the fix

## System Requirements

| Item | Requirement |
|------|------|
| OS | Debian 13 (Trixie) |
| CPU | 1 core or more |
| Memory | 1 GB or more (swap is auto-created if below) |
| Architecture | x86_64 |

> Cloud-provider-modified images may cause unexpected issues. If you encounter installation difficulties, consider using [bin456789/reinstall](https://github.com/bin456789/reinstall) to reinstall a clean Debian 13 and try again.

## Why These Technology Choices

**Why Debian 13?**

Debian is one of the most stable distributions in the server space. Trixie (Debian 13) was the latest stable release when panel development began, offering the newest kernel and reasonably up-to-date package versions while maintaining Debian's traditionally conservative stability policy. Choosing this version means the panel benefits from long-term security update support, and users do not need to upgrade their system frequently.

**Why Pin PHP 8.3?**

WordPress officially recommends PHP 8.3 or higher. PHP 8.3 has undergone the most extensive production validation in the WordPress ecosystem, has an active support lifecycle, and continues to receive performance and security improvements. Pinning the version means all users run the same PHP environment — issues are reproducible and diagnosable, avoiding mysterious compatibility bugs from PHP version differences.

**Why MariaDB Instead of MySQL?**

WordPress officially recommends MariaDB 10.6 or higher. The MariaDB version included with Debian 12/13 meets this requirement. Oracle MySQL carries license and feature restriction risks; MariaDB is a fully compatible, community-driven GPL fork. The MariaDB LTS version from the Debian repository provides security updates through 2028, with no need to add third-party repositories.

**Why a Custom Go Binary Instead of Docker/PM2?**

A single binary with zero dependencies, managed by `systemd`. It uses only a dozen or so MB of memory, making it ideal for 1 GB VPS instances. It does not share ports with Nginx — each provides HTTPS independently. No container layer, no runtime overhead.

## Runtime Components

All components are installed via the APT package manager; the panel does not compile its own:

| Component | Description |
|------|------|
| PHP 8.3 | Ondřej Surý repository, isolated FPM Pool per site |
| MariaDB | Debian-included LTS version |
| Nginx | Debian-included stable version |
| Redis | Debian-included version |
| Fail2ban + nftables | Debian-included versions |

## Technical Architecture

- **Backend**: Go + Gin Web Framework, SQLite (WAL mode), port 8443 (HTTPS/TLS)
- **Frontend**: HTML templates + TailwindCSS + Alpine.js + Chart.js
- **Distribution**: Single binary (frontend assets embedded via `//go:embed`), ~20 MB
- **Security**: The panel is not coupled to an Nginx reverse proxy; it provides independent TLS encryption

## SSH Management Commands

After installation, the panel provides the `wp` command-line tool:

| Command | Description |
|------|------|
| `wp` | View panel information |
| `wp restart` | Restart the panel |
| `wp password` | One-click reset of admin credentials |
| `wp info` | View version/port/entry path |
| `wp status` | View runtime status |
| `wp unban` | Clear all IP bans (emergency recovery when admin is mistakenly banned) |

## Panel Database Backup & Recovery

The panel stores data in SQLite and auto-backs up daily at 2:30 AM to `/www/server/panel/backups/panel-db/`, retaining the 7 most recent copies.

### When the Panel is Working

On the "Panel Settings" page you can:
- Manually create a backup
- Download backup files locally
- Restore from backup (a safety backup is auto-created before restore; the panel auto-restarts after)
- Delete backups

### Recovery Steps When the Panel Won't Start

If the panel fails to start after a database restore, or a corrupted database prevents the panel from running, manually recover via SSH:

```bash
# 1. List available backups
ls -lh /www/server/panel/backups/panel-db/

# 2. Stop the panel
systemctl stop wp-panel

# 3. Back up the current corrupted database (just in case)
cp /www/server/panel/panel.db /www/server/panel/panel.db.broken

# 4. Replace the current database with a backup (use the actual backup filename)
cp /www/server/panel/backups/panel-db/panel_20260107_023000.db /www/server/panel/panel.db

# 5. Start the panel
systemctl start wp-panel

# 6. Verify it is running
systemctl status wp-panel
journalctl -u wp-panel -n 20
```

### Importing Backups After Reinstalling the Panel

If you need to fully reinstall the panel and restore data:

```bash
# 1. Save backup files to a safe location first
cp -r /www/server/panel/backups/panel-db/ /root/panel-db-backup/

# 2. Reinstall the panel (choose "Uninstall then reinstall" to preserve site data)

# 3. Stop the panel after installation
systemctl stop wp-panel

# 4. Replace the new database with the backup
cp /root/panel-db-backup/panel_20260107_023000.db /www/server/panel/panel.db

# 5. Start the panel (auto-runs database migration)
systemctl start wp-panel
```

> **Note**: Older backups may lack newer database fields; the panel will auto-complete them via the migration chain on startup.

## Project Structure

```
├── main.go               # Program entry point
├── config/               # Global configuration management
├── database/             # SQLite connection & migration
├── models/               # Data structures
├── router/               # Routing + page dispatch
├── middleware/            # BasicAuth / Session / CSRF / login rate limiting
├── handlers/             # HTTP handlers
├── executor/             # Task executor
├── collector/            # System metrics collection
├── templates/            # HTML templates
├── static/               # JS
├── input.css             # TailwindCSS source file
├── install.sh            # One-click install script
├── install-cn.sh         # China-optimized install script
├── security/             # Security documentation
└── wp-panel-optimizer/   # WordPress companion plugin
```

## License

GPL-3.0
