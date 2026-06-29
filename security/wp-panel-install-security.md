# WP Panel Install Script Security Transparency Report

> **Subtitle**: A section-by-section breakdown of `install.sh`, addressing false claims of "tampering with passwords, deleting Nginx, hacking WordPress"

---

## 1. Foreword: The Conclusion First

Someone claimed in a GitHub Issue:

> "Server password was directly tampered with, all wp sites jumped to the periodic table hahaha, nginx was also deleted"

This is a **completely falsifiable claim**. WP Panel's install scripts `install.sh` and `install-cn.sh` are both **open source files** that anyone can open and read line by line. This article breaks the entire installation process into over a dozen independent steps, explaining what each step does, why it is needed, and why it **cannot** perform the alleged actions.

**Core conclusions up front**:

| Claim | Actual Script Behavior | Can It Do What Is Claimed? |
|------|------------------|----------------------|
| Tamper with server root/SSH passwords | **Does not touch** `/etc/shadow`, `/etc/ssh/sshd_config`, or system user infrastructure | ❌ Impossible |
| Hack WordPress (tamper with files) | Only downloads the official ZIP from `wordpress.org` as a spare; does **not read, modify, or scan** existing sites | ❌ Impossible |
| Delete Nginx | **Installs and enables** Nginx; even during uninstall, site data is explicitly preserved | ❌ Impossible |

---

## 2. `install-cn.sh`: Just an "Entry Greeter"

Let's start with the simpler one. The `install-cn.sh` that users in China see is still very short; its core logic is just three lines:

```bash
export WP_PANEL_PREFER_CN_MIRROR=1    # Flag: prefer China mirrors
bash install.sh --prefer-cn            # Call the main script with China-preferred flag
```

If `install.sh` exists in the same directory, it executes the local file; if not, it tries a fixed whitelist of sources in order — `gh.wp-panel.org`, `jsDelivr`, GitHub direct — to fetch the main script, and moves to the next source if the content appears abnormal.

**Security points**:
- It performs no system operations; it is merely a "**trampoline script**".
- You can save the fetched `install.sh` content locally, review it, then execute it.
- There is no scenario of "two scripts doing different things and hiding malicious logic".

---

## 3. `install.sh` Panorama: Understanding 1000 Lines of Script in One Table

`install.sh` may be long, but its structure is very clear. It can be divided into the following functional modules (line numbers may shift slightly between versions; refer to current source):

| Module | Line Range | What It Does | Involves Passwords/Deletion? |
|------|----------|--------|----------------------|
| Initialization & argument parsing | 1–144 | Color output, log functions, parsing `--prefer-cn` and other arguments | ❌ No |
| System kernel tuning | 53–120 | TCP tuning, BBR, file descriptor limits | ❌ No |
| Debian / PHP source configuration | 150–282 | Select Debian mirror and add PHP 8.3 source (USTC/SJTU/Official) | ❌ No |
| Uninstall / cleanup functions | 288–397 | Defines uninstall logic (not executed during install) | ⚠️ Only during uninstall |
| Reinstall detection | 400–491 | Checks if already installed; offers repair/uninstall options | ❌ No |
| Swap configuration | 494–517 | Auto-creates 2GB swap if memory ≤ 1GB | ❌ No |
| APT base component install | 519–569 | Installs nginx, mariadb, redis, fail2ban, PHP extensions, etc. | ❌ Installs, does not delete |
| systemd guardian | 572–589 | Configures auto-restart for nginx/php/mariadb/redis on crash | ❌ No |
| Nginx base configuration | 592–616 | Writes rate limiting, FastCGI cache config | ❌ No |
| Firewall: open port 8443 | 619–634 | nftables/ufw open the panel port | ❌ Only opens one port |
| MariaDB security hardening | 637–680 | Set root password, delete empty users, drop test DB, disable remote root | ⚠️ Sets MariaDB password, not system password |
| Directory structure & permissions | 683–691 | Creates `/www/server/panel` and other dirs, permissions 700 | ❌ No |
| Self-signed SSL certificate | 694–711 | Locally generates 2048-bit RSA cert, 10-year validity | ❌ Generated locally, no network |
| Download WordPress package | 714–732 | Downloads the latest official ZIP from wordpress.org | ❌ Download only, as a spare |
| Generate panel security credentials | 735–760 | Generates random passwords from /dev/urandom, stores as bcrypt hash | ⚠️ Generates panel's own passwords |
| Write config.json | 763–826 | Writes panel config to JSON file, permissions 600 | ⚠️ Writes config, no backdoor |
| Deploy panel binary | 829–877 | Downloads or copies compiled Go binary to `/usr/local/bin` | ❌ No |
| Create systemd service | 880–907 | Registers `wp-panel.service`, auto-start on boot | ❌ No |
| Port check & final output | 910–1013 | Checks if 8443 is listening, prints access URL and passwords | ❌ Output only |

Now, let's dive into each section in detail.

---

## 4. Section-by-Section Breakdown: What Each Line Does

### 4.1 Permission Check & Reinstall Detection (lines 400–491)

```bash
if [[ $EUID -ne 0 ]]; then
    log_error "Please run this script with root privileges"
fi
```

**Why is root needed?** Because the panel needs to install system packages (nginx, mariadb), write to `/etc/nginx`, and manage system services. This is a common requirement for any server panel (including Baota, cPanel), not a special design choice of WP Panel.

**Security design highlights**:
- If an existing installation is detected, the script **stops and asks**, rather than forcefully overwriting. Four options are offered: uninstall & reinstall / uninstall only / full purge / exit.
- If an incomplete previous installation is detected (with leftover files), it also asks whether to continue & repair, clean & reinstall, or exit.
- **There is no silent overwrite or stealth reinstall behavior.**

### 4.2 System Kernel Tuning (lines 53–120)

```bash
cat > /etc/sysctl.d/99-wp-panel.conf << 'SYSCTLEOF'
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 8192
# ... TCP buffers, TIME-WAIT, Keepalive, BBR, etc.
SYSCTLEOF
```

What is written here is a set of **publicly standard Linux network tuning parameters**, commonly found in all web server optimization guides. Then `sysctl --system` is executed to apply them.

- It modifies **network stack parameters**, not passwords.
- On single-core VPS, BBR is intelligently skipped to avoid CPU contention.
- The filename `99-wp-panel.conf` is chosen for easy identification and future cleanup.

### 4.3 PHP 8.3 Source Configuration (lines 150–282)

The PHP version in the Debian 13 official repository is 8.4, while WordPress recommends PHP 8.3, which currently has better ecosystem compatibility. Therefore, Ondřej Surý's PHP source must be added to support PHP 8.3 installation. The script provides triple fallback, with China mode prioritizing domestic mirrors:

1. **USTC Mirror**
2. **SJTU Mirror**
3. **Official Source** (`packages.sury.org`, last resort)

```bash
# Download and install GPG public key (for verifying package signatures)
download_file "$PHP_KEY_URL" "$tmp_key" 20
dpkg -i "$tmp_key"

# Write apt source list
cat > /etc/apt/sources.list.d/php.sources << PHPSOURCESEOF
Types: deb
URIs: ${PHP_REPO_URL}
Suites: ${codename}
Components: main
Signed-By: ${keyring_file}
PHPSOURCESEOF
```

**Security points**:
- All PHP packages are installed via `apt`, protected by GPG signatures.
- The script first runs `apt-get update` and verifies whether `php8.3-cli` and `php8.3-fpm` have candidate versions; if **unavailable, it tries the next source**, only aborting after all fail.
- No remote code execution or password modification is involved.

### 4.4 Base Component Installation (lines 547–569)

```bash
apt-get install -y \
    nginx \
    mariadb-server \
    redis-server \
    fail2ban \
    nftables \
    php8.3-fpm php8.3-mysql php8.3-curl ...
```

**This is installing software, not deleting it.** What is installed:
- **nginx**: web server
- **mariadb-server**: database
- **redis-server**: cache
- **fail2ban**: intrusion prevention (auto-bans brute-force IPs)
- **nftables**: firewall framework
- **php8.3-\***: PHP and common extensions

These are all standard packages from the official Debian repository, with public versions and verifiable signatures.

In China mode, the script tests Nanjing University, USTC, Tsinghua, and Debian official sources in order before installing base components, and writes `debian`, `debian-security`, and `debian-updates` entries. Each source runs `apt-get update` first, then checks whether critical packages like `nginx`, `mariadb-server`, `redis-server`, and `ca-certificates` have candidate versions. If a China mirror is unavailable or incompletely synced, it warns "China mirror sync may be delayed; falling back to official source."

### 4.5 systemd Process Guardian (lines 572–589)

```bash
for svc in nginx php8.3-fpm mariadb redis-server; do
    mkdir -p "/etc/systemd/system/${svc}.service.d"
    cat > "/etc/systemd/system/${svc}.service.d/wp-panel.conf" << SYSTEMDEOF
[Service]
Restart=always
RestartSec=5s
StartLimitIntervalSec=0
SYSTEMDEOF
done
```

Adds an **override configuration** for nginx, php-fpm, mariadb, and redis: if a process crashes unexpectedly, it auto-restarts after 5 seconds. This is a standard system stability operation; it does not change any service data or passwords.

### 4.6 Nginx Base Configuration (lines 592–616)

The script writes two config files:

1. **`/etc/nginx/conf.d/wppanel-ratelimit.conf`** — Request rate limiting:
   - Logged-in WordPress users (with `wordpress_logged_in` cookie) are **not rate-limited**
   - Non-logged-in access limited to **60 requests/minute**
2. **`/etc/nginx/conf.d/wppanel-cache.conf`** — FastCGI cache path config

Then `nginx -t` is run to test config validity, followed by `nginx -s reload` for graceful reload.

**Security significance**: This is **defensive hardening**, not sabotage.

### 4.7 Firewall: Opening Port 8443 (lines 619–634)

```bash
# nftables
nft add rule inet filter input tcp dport 8443 accept

# ufw
ufw allow 8443/tcp
```

It does exactly **one thing**: opens the panel's HTTPS management port `8443`. It does not close ports 22 (SSH), 80, 443, etc. Later, the panel's scan defense module will further harden firewall rules.

### 4.8 MariaDB Security Hardening (lines 637–680)

This is one of the **few places involving passwords** in the install script, but it only involves MariaDB (the database), not the system root password or SSH password.

```bash
# Generate a 32-character random password
MYSQL_PASS=$(head -c 24 /dev/urandom | sha256sum | head -c 32)

# If MariaDB has no password set, set one
mysqladmin -u root password "${MYSQL_PASS}"

# Execute MariaDB official security hardening: delete empty users, drop test DB, disable remote root
mysql -u root -p"${MYSQL_PASS}" -e "
    DELETE FROM mysql.user WHERE User='';
    DELETE FROM mysql.user WHERE User='root' AND Host!='localhost';
    DROP DATABASE IF EXISTS test;
    DELETE FROM mysql.db WHERE Db='test' OR Db='test\\_%';
    FLUSH PRIVILEGES;
"
```

**Security points**:
- The MariaDB root password is randomly generated from `/dev/urandom` — **not a hardcoded universal password**.
- If MariaDB already has a password and is reachable, the script **reuses the existing password** and does not forcibly change it.
- **Remote root login** is disabled; only localhost connections are allowed.
- It **does not touch** the Linux system root password, `/etc/passwd`, or `/etc/shadow` at all.

### 4.9 Directory Structure & File Permissions (lines 683–691)

```bash
mkdir -p "$INSTALL_DIR"/{backups,packages,logs,certs}
mkdir -p /www/wwwroot
mkdir -p /www/wwwlogs
mkdir -p /www/server/certificates
chmod 700 "$INSTALL_DIR"       # Only the owner can read, write, and execute
```

Creates the standard directory structure. Key permission settings:
- Panel data directory `/www/server/panel` set to `700`: only root can enter.
- Later, `config.json` is set to `600`: only root can read/write.

### 4.10 Self-Signed SSL Certificate (lines 694–711)

```bash
openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
    -keyout "$KEY_FILE" \
    -out "$CERT_FILE" \
    -subj "/C=CN/ST=Shanghai/L=Shanghai/O=WP Panel/OU=IT/CN=WP-Panel-SelfSigned" \
    -addext "subjectAltName=IP:127.0.0.1"
```

- **Generated locally on the server**; no connection to any external CA or API.
- 2048-bit RSA, 10-year validity.
- Private key file permissions `600`, certificate `644`.
- After installation, you can replace it with your own official certificate at any time; the panel supports configuring the certificate path.

### 4.11 Download WordPress Official Package (lines 714–732)

```bash
download_file "https://wordpress.org/latest.zip" "$WP_ZIP_TMP" 60
```

- The download source is **`wordpress.org`** — the official WordPress website, not any third-party repository.
- The downloaded ZIP is **saved to `/www/server/panel/packages/` as a spare**, for use in subsequent "one-click site creation".
- If the download fails, the script notes "will download on first site creation" — **site creation will not fail**.
- It does **not scan, read, or modify** any existing WordPress sites on the server.

### 4.12 Generate Panel Security Credentials (lines 735–760)

This is the **most critical security design** in the install script, involving the generation of two-layer authentication passwords:

```bash
# Read true random data from /dev/urandom
PANEL_SUFFIX=$(head -c 20 /dev/urandom | sha256sum | head -c 8)
BASIC_PASS=$(head -c 12 /dev/urandom | base64 | head -c 16)
WEB_PASS=$(head -c 12 /dev/urandom | base64 | head -c 16)

# Hash with bcrypt using PHP or Python (cost=12)
BASIC_HASH=$(php8.3 -r "echo password_hash('$BASIC_PASS', PASSWORD_BCRYPT, ['cost' => 12]);")
WEB_HASH=$(php8.3 -r "echo password_hash('$WEB_PASS', PASSWORD_BCRYPT, ['cost' => 12]);")
```

**Layer-by-layer analysis**:

1. **Entropy source**: `/dev/urandom` is a cryptographically secure random number generator provided by the Linux kernel, not the pseudo-random function `rand()`.
2. **Password complexity**: 8-character random suffix + 16-character random BasicAuth password + 16-character random Web password. Brute-forcing is infeasible.
3. **Storage**: Only **bcrypt hash values** (starting with `$2a$12$`) are stored in the config file — **no plaintext passwords**.
   - bcrypt cost=12 means brute-forcing a single password takes years.
   - Even if someone obtained your `config.json`, they **cannot reverse-engineer the original password**.
4. **Degradation protection**: If the server has neither PHP 8.3 nor Python 3 (extremely rare), the script writes a placeholder hash and notes "panel will auto-reset password on first startup". **It will never start with a weak or empty password**.

### 4.13 Writing `config.json` (lines 763–826)

All configuration is written to a single JSON file:

```json
{
  "panel": { "port": 8888, "tls_port": 8443, "random_suffix": "abc123de" },
  "mariadb": { "root_password": "xxx..." },
  "admin": { "username": "wpadmin", "password_hash": "$2a$12$..." },
  "basic_auth": { "username": "admin", "password_hash": "$2a$12$..." },
  "security": { "max_login_attempts": 5, "ban_duration_hours": 24 }
}
```

**Security points**:
- File permissions `600`: only root can read.
- The MariaDB root password is stored here, but the panel itself **only connects to MariaDB via localhost + Unix socket** and does not expose it externally.
- **There are no hidden backdoor accounts, no hardcoded universal keys, no logic to send passwords to external servers**.

### 4.14 Deploy Panel Binary (lines 829–877)

```bash
# Prefer local binary in the same directory
cp "$SCRIPT_DIR/wp-panel" "$BIN_PATH"

# Otherwise download from GitHub Release
GITHUB_RELEASE="https://github.com/CalvinSmall/wp-panel-en-private/releases/latest/download/wp-panel"
```

- If you place the compiled `wp-panel` binary in the same directory as the install script, the script **uses the local file directly** without downloading from the network.
- If a download is needed, the source is **GitHub Releases** (official releases from the open-source repository), with an optional `gh.wp-panel.org` China proxy.
- After download, `chmod +x` grants execute permission, then it is moved to `/usr/local/bin/wp-panel`.

**How to verify the binary is safe?**
- WP Panel is an **open-source project**; you can clone the code, audit the Go source, compile locally, and use your own build.
- The install script provides a local deployment path; you can **install fully offline**.

### 4.15 Creating the systemd Service (lines 880–907)

```bash
cat > "$SERVICE_PATH" << SYSTEMDEOF
[Unit]
Description=WordPress Server Management Panel
After=network.target mariadb.service redis-server.service

[Service]
Type=simple
User=root
Group=root
ExecStart=$BIN_PATH --config=$CONFIG_FILE
Restart=always
RestartSec=5
LimitNOFILE=65536
SYSTEMDEOF
```

- Runs as `root` because the panel needs to manage Nginx configs, restart services, and operate on the filesystem. This is standard practice for server panels.
- `Restart=always`: if the panel process crashes, it auto-restarts after 5 seconds.
- Then `systemctl enable wp-panel` (auto-start on boot) and `systemctl start wp-panel` (start immediately) are executed.

### 4.16 Port Check & Final Output (lines 910–1013)

Finally, the script will:
1. Check if the `wp-panel` service is running
2. Check if port 8443 is listening
3. **Print the access URL and both-layer passwords (shown only once)**

```
Panel URL: https://<IP>:8443/<random suffix>/
Layer 1 — BasicAuth (browser prompt)  Username: admin / Password: xxxxxxxx
Layer 2 — Web Login (panel form)   Username: wpadmin / Password: xxxxxxxx
```

**Security points**:
- Passwords are **output to the terminal only once**; they are not saved to logs or sent to any remote server.
- The script mentions "anonymous install stats" at the end, whose content is limited to: an anonymous machine identifier (SHA256 hash of `/etc/machine-id`) + panel version number. **No IP, domain, or password is included.**

---

## 5. Direct Response to the Three Claims

### Claim 1: "The server password was tampered with"

**Fact**: The install script **never reads or writes** any of the following files:

- `/etc/shadow` (Linux user passwords)
- `/etc/passwd` (user list)
- `/etc/ssh/sshd_config` (SSH configuration)
- `~/.ssh/authorized_keys` (SSH public keys)

The only passwords the script touches are:
1. **MariaDB root password** — randomly generated, used for the panel to manage the database; it is not the system root password.
2. **The panel's own two-layer login passwords** — randomly generated, stored as bcrypt hashes, completely unrelated to system passwords.

> Your SSH root password **does not change** before or after installation. If you find your password has changed, check: whether you used a weak password that was brute-forced, whether your keys were leaked elsewhere, or whether other unknown software was installed.

### Claim 2: "All WP sites redirected to the periodic table"

(Note: "the periodic table" refers to defaced pages or phishing content displayed after a site is hacked.)

**Fact**: The install script **does not touch** any existing files under `/www/wwwroot/` at all. The only WordPress-related operation in the script is:

```bash
download_file "https://wordpress.org/latest.zip" "$WP_ZIP_TMP" 60
```

- Download a ZIP from the **official WordPress website** to `/www/server/panel/packages/wordpress.zip`
- This is a **spare package**, to be extracted during subsequent "one-click site creation"
- **It will not be automatically extracted into any existing directory**
- It will **not scan, modify, or delete** any files of existing sites

> If your WordPress site was defaced, common causes are: using cracked plugins/themes, unpatched WordPress core or plugin vulnerabilities, or weak passwords being brute-forced. This has nothing to do with the WP Panel install script.

### Claim 3: "Nginx was also deleted"

**Fact**: The install script **installs and enables** Nginx:

```bash
apt-get install -y nginx
systemctl start nginx
systemctl enable nginx
```

It then writes rate limiting and cache config, and runs `nginx -t` (config check) and `nginx -s reload` (graceful reload).

Even when performing **uninstall** (the `do_uninstall` function), the script explicitly preserves:

```bash
log_info "Panel uninstalled. The following have been preserved:"
log_info "  - /www/wwwroot (website files)"
log_info "  - /www/wwwlogs (website logs)"
log_info "  - /www/server/certificates (SSL certificates)"
log_info "  - MariaDB databases"
log_info "  - System packages (nginx/php/mariadb/redis/fail2ban)"
```

Only when the user **manually chooses "full purge"** will nginx and other packages be removed — and this requires **interactive confirmation**, by typing `yes`.

> If you find Nginx was deleted, check: whether you manually ran uninstall or purge, or whether other management scripts/personnel are operating on the server.

---

## 6. What You Can Verify Yourself

Open source means not just "code is public", but also "you can personally verify". Here are a few simple self-check methods:

### Method 1: Audit the Install Script (without executing it)

```bash
# Download first, review, do not execute
curl -fsSL https://raw.githubusercontent.com/CalvinSmall/wp-panel-en-private/main/install.sh -o install.sh
# Open in a text editor, search for these keywords:
# passwd, shadow, ssh, rm -rf /etc/nginx, wget to non-wordpress.org addresses
# You will find: the above keywords either do not exist or appear in safe contexts.
```

### Method 2: Offline Local Installation

```bash
# 1. Clone source and compile locally
git clone https://github.com/CalvinSmall/wp-panel-en-private.git
cd wp-panel
go build -o wp-panel .

# 2. Place the compiled binary together with install.sh
# 3. Disconnect from the internet and run: bash install.sh
# The script will use the local wp-panel binary and will not download any external files.
```

### Method 3: Monitor the Installation Process

```bash
# Open another terminal window to monitor file changes in real time
watch -n 1 'ls -la /etc/shadow; ls -la /www/wwwroot/'

# Also monitor network connections
ss -tpn

# You will find: /etc/shadow timestamp unchanged, /www/wwwroot content unchanged,
# and no unusual external network connections.
```

### Method 4: Inspect config.json After Installation

```bash
cat /www/server/panel/config.json
# Confirm: no hardcoded universal passwords, no reporting endpoints to external servers,
# no suspicious remote command execution config.
```

---

## 7. Conclusion

WP Panel's `install.sh` is essentially an **automated server operations manual**: everything it does — install Nginx, configure MariaDB, tune the kernel, generate random passwords, set up firewall rules — are standard operations that a seasoned sysadmin would perform manually when setting up a WordPress server.

The benefit of open-sourcing all of this: **there is zero room for black-box operations**. Every claim can be verified as true or false by reading the source code, comparing file hashes, and monitoring the installation process.

| Rumor | Truth |
|------|------|
| Tamper with server passwords | ❌ Script does not touch `/etc/shadow`, `/etc/ssh`, or any system authentication infrastructure |
| Hack WordPress | ❌ Only downloads the official spare package from wordpress.org; does not touch existing sites |
| Delete Nginx | ❌ Installs and enables Nginx; even during uninstall, site data is explicitly protected |

If you still have concerns, you are welcome to:
1. Open `install.sh` and search for the key line numbers mentioned in this article to personally verify.
2. Perform an offline installation in a local VM or container and observe every step.
3. Read the second article in this series: "How WP Panel Protects Your Server Through Multi-Layered Mechanisms After Installation".

---

*This article is based on `install.sh` and `install-cn.sh` from the WP Panel open-source repository commit history. All line numbers and code snippets are publicly verifiable.*
