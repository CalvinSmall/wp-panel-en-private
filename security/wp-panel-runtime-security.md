# How WP Panel Protects Your Server Through Multi-Layered Mechanisms After Installation

> **Subtitle**: Addressing "Can the panel secretly change my password or plant a trojan?" and "Is the panel safe when my server credentials haven't leaked?"

---

## 1. Foreword

In Part 1, we proved: WP Panel's install script is transparent — it won't tamper with your server passwords, hack WordPress, or delete Nginx. But after installation, new questions naturally arise:

- **After the panel is running, will it secretly change my password?**
- **The panel has auto-update — could it be hijacked to plant a trojan?**
- **My SSH password hasn't leaked — is the panel itself secure enough?**

This article breaks down all runtime behaviors of WP Panel after installation **at the source code level**, explaining how each **layer of defense** works, and why, without leaked server credentials, an attacker can hardly compromise your server through the panel.

---

## 2. Does the Panel Secretly Change Passwords?

### 2.1 Password Storage: Even the Panel Cannot See Plaintext

WP Panel uses a two-layer authentication system:

| Layer | Purpose | Storage |
|------|------|----------|
| Layer 1 — BasicAuth | Browser prompt; intercepts first-level scanners | bcrypt hash stored in `config.json` |
| Layer 2 — Web Login | Panel form login | bcrypt hash stored in SQLite database |

**What is bcrypt?** It is a **one-way password hashing algorithm**. Your password, after computation, becomes a string starting with `$2a$12$`. The process is **irreversible** — even if someone obtains the database and config file, they **cannot reverse-engineer your original password**.

When verifying login, the panel does only one thing:

```go
bcrypt.CompareHashAndPassword([]byte(stored hash), []byte(password you entered))
```

Match → pass; mismatch → reject. **The panel never saves, prints, or transmits your plaintext password.**

### 2.2 Under What Circumstances Can the Password Be Changed?

There are only **three** code paths that can change the password, all of which require **your explicit action**:

| Method | Trigger | Who Can Execute |
|------|----------|------------|
| Panel settings page | Login to panel → Settings → Change password | Admin who knows current password |
| CLI one-click reset | Run `wp password` via SSH | Server root user |
| CLI change password, keep username | Run `wp-panel --passwd="new password"` via SSH | Server root user |

**Why does typing `wp password` actually run a different command?**

`wp` is a **command wrapper script** created during panel installation (stored at `/usr/local/bin/wp`). Its purpose is to wrap complex low-level commands into easy-to-remember everyday instructions. The mapping is as follows:

| Command You Type | Actual Low-Level Command | Difference |
|-------------|-------------------|----------|
| `wp password` | `wp-panel --reset-admin` | **Resets both username and password** (username reverts to `wpadmin`, password is randomly generated) |
| `wp-panel --passwd="xxx"` | `wp-panel --passwd="xxx"` | **Only changes password; keeps current username** |

In other words, `wp password` is better for emergency situations like "I'm locked out, one-click recovery"; while `wp-panel --passwd="xxx"` is for scenarios like "I know my current username but just want to change the password." Both are real — one is a human-friendly shortcut, the other is a low-level interface for fine-grained control.

**Key conclusions**:
- No scheduled task auto-changes passwords in the middle of the night.
- No remote command can change your password over the air.
- There is no "backdoor password" or "universal key."
- The panel **sends** your password or password hash **to no server**.

### 2.3 If Your Password Really Was Changed, More Likely Causes Are…

If your server password was changed after installing the panel, the investigation order should be:

1. **Were SSH keys leaked** — Check `~/.ssh/authorized_keys` for unfamiliar public keys
2. **Was a weak password used** — Is the server root password in a wordlist
3. **Was other software installed** — Are there suspicious processes beyond the panel
4. **Was the cloud provider console compromised** — Password reset via VNC/console leaves no trace

> **Not a single line** of panel code touches `/etc/shadow`, `/etc/passwd`, or SSH configuration. This is verifiable by full-text search.

---

## 3. Can the Panel Inject Viruses or Trojans?

This is the most critical and most reasonable concern. A persistent background program with auto-update capability theoretically does present an exploit risk. We need to analyze this from two dimensions: the **update mechanism** and **runtime constraints**.

### 3.1 Auto-Update Mechanism: Three Independent Verifications

The update functionality of WP Panel is implemented in `handlers/update.go`, with the following flow:

```
User clicks "Update Panel" → Download new binary → SHA256 check → Ed25519 signature verification → Preflight → Backup old version → Replace → Restart
```

#### First Line: SHA256 Integrity Check

After download, the panel also downloads a `.sha256` file containing the correct hash. The panel recomputes the SHA256 of the downloaded file and **aborts immediately on mismatch**.

```go
if err := verifySHA256(newBinary, shaFile); err != nil {
    fail(http.StatusInternalServerError, "Verification failed")
    return
}
```

This ensures the file was not tampered with or corrupted during transmission.

#### Second Line: Ed25519 Digital Signature

SHA256 only guarantees file integrity but cannot prove file origin. Therefore, the panel also introduces **Ed25519 asymmetric signatures**:

- **Public key** is hardcoded in the panel source code (`releasePubKeyHex = "ee8ec641..."`)
- **Private key** is kept offline by the author — not on GitHub, CI, or any server
- On each release, the author signs the `.sha256` file with the private key, producing `.sha256.sig`
- The panel verifies the signature with the built-in public key; **if verification fails, the update is aborted**

```go
if err := verifyEd25519(shaFile, sigFile); err != nil {
    fail(http.StatusInternalServerError, "Signature verification failed")
    return
}
```

**What does this mean?** Even if an attacker hijacked GitHub Releases and uploaded a malicious binary, without the author's private key they **cannot generate a valid Ed25519 signature**, and the panel will refuse to install it.

#### Third Line: Preflight

Even after passing the first two checks, the panel runs a **preflight** before replacing:

```go
if err := preflightBinary(newBinary); err != nil {
    fail(http.StatusInternalServerError, "New version preflight failed")
    return
}
```

The preflight runs the new binary with the `--info` flag to confirm it can start normally and won't crash on launch. If the new binary has been tampered with into a non-functional junk file, this step catches it.

#### Backup & Rollback

Only after all the above verifications pass does the panel perform the replacement, and it **always backs up** before replacing:

```go
backupPath := versionedBackupPath(h.CurrentVersion)  // /usr/local/bin/wp-panel.bak.v1.2.3.20240101-120000
if err := copyFile(installPath, backupPath, 0755); err != nil {
    fail(http.StatusInternalServerError, "Old version backup failed")
    return
}
```

If a permission setting failure is discovered after replacement, the panel **auto-rolls back**:

```go
if err := os.Chmod(installPath, 0755); err != nil {
    if rbErr := copyFile(backupPath, installPath, 0755); rbErr != nil {
        fail(http.StatusInternalServerError, "Post-replacement permission set failed, and auto-rollback also failed")
        return
    }
    fail(http.StatusInternalServerError, "Post-replacement permission set failed; rolled back")
    return
}
```

### 3.2 The Panel Has No "Remote Code Execution" Capability

WP Panel is a **statically compiled binary** written in **Go**, which means:

- It has no runtime interpreter dependency (like PHP, Python, Node.js) and cannot be "script-injected"
- It has no functions like `eval()` or `system()` that can execute arbitrary strings
- All system command invocations go through the **whitelist mechanism** in `executor/commander.go`

How strict is the whitelist? Here are some representative commands (the full whitelist includes 20+ system commands):

| Allowed Command | Allowed Parameters |
|---------------|---------------|
| `systemctl` | start, stop, reload, restart, enable, disable... |
| `nginx` | -t, -s, -c |
| `mysql` | -u, -p, -e, -h, -P |
| `wget` | -q, -O, -T, -t (URL must be HTTPS + whitelisted domain) |
| `curl` | -s, -o, -f, -L, -X, -H, -d (URL must be HTTPS + whitelisted domain) |
| `unzip` | -o, -q, -d |
| `fail2ban-client` | set, unban, status, banip... |

**Dangerous characters are globally filtered**: `;`, `|`, `&`, `` ` ``, `$`, `<`, `>`, and any other characters that could form shell injection are rejected outright.

```go
func hasUnsafeArgs(binary string, args []string) bool {
    for _, arg := range args {
        if strings.ContainsAny(arg, ";|&`$<>") {
            return true
        }
        // wget/curl URL must be HTTPS and from a whitelisted domain
    }
}
```

### 3.3 File Management Has a "Cage"

The panel provides a file manager, but all file operations are restricted within the **site root directory**:

```go
func isPathWithin(basePath, targetPath string) bool {
    // Resolve symbolic links to prevent escaping the directory via symlinks
    base, _ := filepath.EvalSymlinks(filepath.Clean(basePath))
    target, _ := resolvePathForAccess(targetPath)
    
    // Compute relative path; ensure target is within base
    rel, _ := filepath.Rel(base, target)
    return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
```

**Security points**:
- Even if you are logged into the panel, you **cannot access** sensitive paths like `/etc/shadow`, `/root/.ssh/`, `/www/server/panel/config.json`
- Upload, download, delete, extract, and other operations all pass through `isPathWithin()` checks first; **unauthorized access returns 403 directly**
- Symbolic links are resolved to their real path via `EvalSymlinks`; **cannot bypass directory restrictions with symlinks**

### 3.4 Download Sources Are Locked Down

Every download-related operation in the panel (WordPress core, plugins, themes, update packages) has URLs restricted to a **whitelist of domains**:

```go
func isAllowedDownloadURL(raw string) bool {
    u, err := url.Parse(raw)
    if err != nil || u.Scheme != "https" || u.User != nil {
        return false
    }
    switch strings.ToLower(u.Hostname()) {
    case "wordpress.org", "downloads.wordpress.org", "api.wordpress.org",
         "www.cloudflare.com", "developers.google.com", "www.bing.com":
        return true
    default:
        return false
    }
}
```

- Must use **HTTPS**
- Username/password embedding in URL is not allowed (`u.User != nil`)
- Any domain not on the whitelist is **rejected outright**

---

## 4. How Secure Is the Panel When Server Credentials Haven't Leaked?

Assuming your SSH root password, keys, and cloud provider console have not leaked, an attacker can only reach your server over the network. WP Panel builds **six layers of defense-in-depth** for this scenario.

### 4.1 Concealment Layer: Making the Door Invisible to Attackers

#### Random Entry Path

During installation, the panel generates an **8-character random suffix** (e.g., `a3b9c2d1`), making the panel URL:

```
https://YOUR_IP:8443/a3b9c2d1/login
```

Accessing without this suffix returns a 404. If an attacker wants to brute-force this path:

- The suffix is generated from a SHA256 hash; each position is one of 16 possibilities (0-9, a-f)
- 8-character combinations = 16^8 ≈ **4.3 billion**
- Mathematically, at 10,000 scans/second it would take ~5 days to exhaust — but scan defense bans the IP for 30 days on the very first non-browser request, making a real attack infeasible.

#### Non-Standard Port 8443

The panel uses port 8443 instead of common ports like 80/443/8080/8888, which alone filters out 99% of internet bulk scanners.

#### Scan Defense: Non-Browser Requests Banned for 30 Days

If someone tries to scan port 8443 with a script, the panel's first line of defense is not login verification — it is **`middleware/scan_defense.go`**:

```go
func ScanDefense(db *sql.DB, randomSuffix string) gin.HandlerFunc {
    return func(c *gin.Context) {
        path := c.Request.URL.Path
        // If the path does not start with the random suffix
        if !strings.HasPrefix(path, legitPrefix) {
            // Check if the User-Agent is a browser
            if !isBrowserLike(c) {
                // Not a browser? Ban the IP for 720 hours (30 days)
                banScanIP(db, c.ClientIP(), "High-risk scan: non-browser signature probing panel port", 720)
                c.AbortWithStatus(http.StatusForbidden)
                return
            }
        }
        c.Next()
    }
}
```

**What does this mean?** If an attacker scans your panel port with Nmap, Dirbuster, or a Python script, and the User-Agent lacks browser identifiers like `Mozilla`, `Chrome`, or `Safari`, the **IP is immediately added to the firewall blacklist and banned for 30 days**.

### 4.2 Authentication Layer: Two Password Gates

Even if an attacker knows your random entry, they still need to break through two consecutive authentication layers:

**Layer 1 — BasicAuth (browser prompt)**
- Username/password verified via bcrypt comparison
- 5 consecutive failures → IP banned for 24 hours
- Failure records written to database; fail2ban syncs to system firewall

**Layer 2 — Web Login (panel form)**
- A separate, independent set of username/password
- Also verified via bcrypt comparison
- Same 5-failure ban mechanism
- Session granted only after passing

**Why two layers?** Even if the BasicAuth password is leaked for some reason (e.g., browser remembers the password and someone nearby sees it), the attacker still needs the second-layer password to access the panel. This is not over-engineering — it is **defense-in-depth**.

### 4.3 Session Layer: Stealing the Cookie Is Useless

After a successful login, the panel issues a Session Cookie:

```go
http.SetCookie(c.Writer, &http.Cookie{
    Name:     "wp_session",
    Value:    session.Token,     // UUID random string
    MaxAge:   1800,              // 30 minutes
    Path:     "/",
    HttpOnly: true,              // JavaScript cannot read it
    Secure:   true,              // HTTPS only
    SameSite: http.SameSiteLaxMode,
})
```

**Security features**:
- **HttpOnly**: Even if the site has an XSS vulnerability, JavaScript cannot read this cookie
- **Secure**: Transmitted only via encrypted HTTPS; cannot be intercepted in plaintext by a man-in-the-middle
- **Sliding renewal**: Each access to a valid page auto-extends the validity by 30 minutes
- **Server-side storage**: Sessions are stored in panel memory, **not persisted to disk**. After a panel restart, all sessions are invalidated and re-login is required

Additionally, all **write operations** (modifying configs, deleting files, creating sites, etc.) must also carry a **CSRF Token**:

```go
func CSRF() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Read X-CSRF-Token from header
        headerToken := c.GetHeader("X-CSRF-Token")
        // Read csrf_token from cookie
        cookieToken, _ := c.Cookie("csrf_token")
        // Both must match
        if headerToken != cookieToken {
            c.AbortWithStatusJSON(http.StatusForbidden, ...)
            return
        }
        c.Next()
    }
}
```

This prevents **Cross-Site Request Forgery attacks** — an attacker may trick you into clicking a malicious link but cannot forge the correct CSRF Token.

### 4.4 Transport Layer: Encryption + Security Response Headers

The panel enforces **HTTPS** (port 8443) and sends the following security response headers:

| Header | Purpose |
|--------|------|
| `Strict-Transport-Security: max-age=31536000; includeSubDomains` | Tells the browser to use only HTTPS for the next year |
| `X-Frame-Options: DENY` | Prevents the page from being embedded in an iframe (clickjacking protection) |
| `X-Content-Type-Options: nosniff` | Prevents browser MIME-type sniffing |
| `Referrer-Policy: no-referrer` | Prevents leaking the referring page URL |

### 4.5 Operations Layer: Even Inside the Panel, Harm Is Limited

Consider the extreme case: an attacker has passed all authentication and entered the panel. What can they do?

**File Management**: Can only operate on site files under `/www/wwwroot/` and the backup directory `/www/server/panel/backups`; **cannot access** system config files, other user data, or panel configuration.

**Command Execution**: The panel has no "terminal" feature; all system operations go through encapsulated APIs, which use the `executor/commander.go` whitelist internally. **There is no entry point for arbitrary command input.**

**Database**: The panel itself uses SQLite for config and logs; MariaDB management is performed via the command-line `mysql` client (with the `MYSQL_PWD` environment variable). The MariaDB root password only exists in `config.json` (permissions 600). An attacker via the panel can only manage **site-specific databases** and cannot directly obtain the MariaDB root password.

### 4.6 Network Layer: Firewall + Rate Limiting + Intrusion Detection

**Nginx Rate Limiting**:
- Non-logged-in WordPress users limited to **60 requests/minute**
- Logged-in users are not rate-limited (to avoid blocking legitimate users)

**fail2ban Integration**:
- SSH brute force → auto-ban
- Panel login failure → auto-ban
- 404 scanning → auto-ban

**nftables Firewall**:
- The panel's own scan defense and manual bans write directly to nftables, blocking connections at the system level

---

## 5. Is the Software the Panel Installs Safe? — Software-Level Security

Previous sections proved the panel itself does no harm. But another reasonable concern is: **the software the panel installs (Nginx, MariaDB, Redis, PHP-FPM, etc.) may have vulnerabilities — does the panel do anything about it?**

To answer this, we first need to understand a core concept:

### 5.1 In Plain English: Why "Updating Software" Equals "Fixing Vulnerabilities"

Think of your server as a house; every piece of running software (Nginx, PHP, database, etc.) is a door or a window. **Software vulnerabilities are gaps around the windows** — hackers slip through these gaps into your house.

The key point is:

> **Hackers know about the vulnerabilities — and so do the software vendors. Every "update" the vendor releases fixes these discovered openings.**

An analogy:

| Everyday Scenario | Server Scenario |
|----------|-----------|
| A smart lock brand you installed at home is found to have a security flaw | The Nginx running on your server is found to have a remote code execution vulnerability (CVE) |
| The vendor releases new firmware to fix the flaw | Debian/Nginx officially releases a new version that patches the vulnerability |
| You update the lock firmware → the door is secure again | You run `apt upgrade nginx` → the vulnerability is patched |
| You never update → thieves know this model has a flaw and specifically target these locks | You don't update → hackers scan the entire internet for this Nginx version and easily break in |

**The truth is: the vast majority of hacked sites are not hacked because hackers are brilliant, but because site owners failed to update their software in time.** A 2023 Wordfence report shows that over 60% of compromised sites in the WordPress ecosystem were due to unpatched known vulnerabilities — in other words, keep your software updated and you won't get hacked.

Once you understand this basic logic, it becomes crystal clear what WP Panel does.

### 5.2 What Exactly Is the Panel's "System Update"?

When you open WP Panel's "System Update" page, the panel performs this check in the background:

```
It asks the system: "For all the software we installed (nginx, php, mariadb, redis...), are there new versions?"
```

The system answers:
- No new versions → displays "System is up to date"
- New versions exist → lists upgradable packages with versions, e.g.:
  - nginx 1.24.0 → 1.26.0 (fixed 2 security issues)
  - php8.3-fpm 8.3.6 → 8.3.8 (fixed 1 security issue)
  - mariadb-server 10.11.6 → 10.11.8

Technically, the panel calls the Debian system's `apt list --upgradable` command under the hood (source: `handlers/system_update.go`), which queries the Debian official software repository for the latest version of each package — exactly the same principle as your phone's App Store checking for app updates.

### 5.3 One-Click Update: No Command Line Needed

The traditional approach: SSH into the server → type `apt update` → type `apt upgrade -y` → stare at the screen waiting.

WP Panel turns these three steps into **a single button** (source: `handlers/system_update.go:56-80`). You just open the panel, click "System Update", and the panel completes everything automatically. For site owners who don't know the command line, **no Linux knowledge is needed to keep the server secure**.

### 5.4 Auto-Alerts: When You Forget, the Panel Remembers

This is the most critical yet most often overlooked capability. People forget; the panel does not.

WP Panel's alert system (`executor/alert_monitor.go`) automatically checks every 24 hours:

- **System software update alerts**: Are there new versions of nginx, php, mariadb, redis, etc. for this server? If so, you're reminded.
- **Panel self-update alerts**: Is there a new version of WP Panel itself? You're reminded of that too.

If you've configured email notifications, you'll receive an email like this:

> ⚠️ Your server has 12 available security updates:
> nginx、mariadb-server、php8.3-fpm、php8.3-cli、redis-server、openssl、libssl3...

**What this means**: You don't need to proactively check the CVE database asking "does my nginx version have vulnerabilities" — the Debian security team has already done that for you, they've placed the fixes into apt updates, the panel tells you "updates available", and you just click.

### 5.5 Real-World Scenario: If a Log4j-Level Vulnerability Hits Your Server Components

In late 2021, the Log4j vulnerability (Log4Shell) was disclosed, affecting millions of servers worldwide. Affected companies had to find and update all vulnerable systems within **hours** — a single day's delay could mean compromise.

If your server components (e.g., Nginx or PHP) had an equivalently severe vulnerability, WP Panel's process would be:

```
Vulnerability disclosed → Debian security team releases patch → Panel alert system detects updates →
→ Sends email/Webhook notification → You open the panel → Click "System Update" → Vulnerability patched
```

Compare with the scenario without a panel:
```
Vulnerability disclosed → You have no idea → Weeks later your site gets hacked → You finally find out
```

**The difference is "knowing there's an update" and "how easy it is to apply."**

### 5.6 ProcessGuard: Auto-Restart When Software Crashes

Beyond vulnerabilities, software also has **runtime stability** concerns. The panel's ProcessGuard (`executor/process_guard.go`) checks every 30 seconds whether the following six critical services are alive:

| Service | If It Crashes |
|------|---------|
| Nginx | Sites go down → ProcessGuard auto-restarts it |
| PHP-FPM | Sites show white screen / errors → ProcessGuard auto-restarts it |
| MariaDB | Database unavailable → ProcessGuard auto-restarts it |
| Redis | Cache invalidated, site slows down → ProcessGuard auto-restarts it |
| nftables | Firewall down → ProcessGuard auto-restarts it |
| Fail2ban | Brute-force protection down → ProcessGuard auto-restarts it |

For beginners, **you don't even need to know what these services are called** — the panel silently guards them in the background. You can see each service's status on the "System Guardian" page: green = normal, red = auto-restarted.

### 5.7 Software Version Transparency

The panel displays the exact version number of every installed piece of software on the "Software Management" page (`handlers/software.go`). If a serious vulnerability is disclosed one day, you can immediately confirm whether your server is affected:

- CVE advisory says "Nginx versions before 1.24.0 are affected" → You check the panel: mine is 1.26.0 → Not affected, rest easy
- CVE advisory says "PHP 8.3.0–8.3.7 are affected" → You check the panel: mine is 8.3.1 → Affected → One-click update immediately

No need to type commands to check versions, no need to memorize software paths — all visible on one page.

### 5.8 Are These Software Sources Reliable?

All software installed by WP Panel comes from the **Debian official repository** or the **Ondřej Surý PHP official source** (source: `install.sh:549-568`); the panel does not package its own:

```
apt-get install -y nginx mariadb-server redis-server fail2ban nftables php8.3-fpm ...
```

- **Debian official repository** is maintained by the Debian security team; every package has a GPG digital signature, ensuring it hasn't been tampered with
- **Ondřej Surý PHP source** is the most authoritative third-party PHP source in the Debian ecosystem, also with GPG signature verification

The panel's role is not to "provide software", but to "manage these official packages for you and notify you the moment security updates are available."

### 5.9 One-Sentence Summary

| Your Concern | The Reality |
|----------|---------|
| "What if Nginx installed by the panel has a vulnerability?" | Vulnerability exists → Debian official releases update → Panel auto-detects it → Sends email/Webhook to notify you → You click one-click update → Vulnerability fixed. No technical knowledge needed. |
| "I don't know when I should update" | The panel knows for you. Auto-checks every 24 hours; notifies you when updates are available. |
| "I don't know how to update" | The panel handles it. One button, no command line needed. |
| "What if software crashes?" | The panel restarts it for you. Auto-recovers within 30 seconds; you might not even notice. |

WP Panel does not introduce new vulnerabilities into the software — it installs from official sources, with every package signature-verified. What the panel does is **let you know about vulnerabilities faster and fix them more easily than manual management ever could**.

---

## 6. Common Attack Scenario Simulations

To understand the security more intuitively, we simulate several common attack methods:

### Scenario 1: Brute-Forcing Panel Login

**Attacker's approach**: Dictionary attack on `https://IP:8443/random_suffix/login`

**Result**:
- Unknown random suffix → accessing `/` returns 404; scan defense triggers and IP is banned for 30 days
- Known random suffix but unknown BasicAuth password → after 5 failures, IP banned for 24 hours
- Broke through BasicAuth but unknown Web password → another 5 failures, banned another 24 hours
- At 1 attempt per second, cracking a 16-character random password takes **hundreds of millions of years**

**Conclusion**: Pure network brute-force is **infeasible**.

### Scenario 2: SQL Injection

**Attacker's approach**: Input `' OR '1'='1` in the login form

**Result**: The panel uses parameterized queries:

```go
db.QueryRow("SELECT password_hash FROM admin_users WHERE username = ?", req.Username)
```

User input is treated as a **plain text parameter** and is not parsed as a SQL statement.

**Conclusion**: SQL injection is **infeasible**.

### Scenario 3: Path Traversal (downloading `/etc/passwd`)

**Attacker's approach**: Access `../../../etc/passwd` via the file management API

**Result**: The `isPathWithin()` function computes the relative path, discovers the target falls outside the site root, and returns a **403 Path Traversal** directly.

**Conclusion**: Path traversal is **infeasible**.

### Scenario 4: Command Injection (typing `; rm -rf /` in a domain input)

**Attacker's approach**: Input a malicious domain during site creation

**Result**: All system command operations are filtered through `executor/commander.go`:
- The command must be on the whitelist
- Parameters must not contain `;|&\`$<>`
- Modes like `bash -c` that can execute arbitrary strings **simply do not exist**

**Conclusion**: Command injection is **infeasible**.

### Scenario 5: XSS (Cross-Site Scripting)

**Attacker's approach**: Change the panel title to `<script>alert(1)</script>` in "Panel Settings"

**Result**:
- Backend templates use Go's `html/template`, which **auto-escapes** HTML special characters; `<script>` is rendered as plain text, not executable script
- Frontend data is transmitted via API JSON; the browser renders it as text
- Even if the frontend is bypassed, cookies are HttpOnly; JS cannot read the Session

**Conclusion**: XSS cannot steal login credentials.

---

## 7. Does the Panel Really Not "Secretly Phone Home"?

### 7.1 Telemetry Reporting: Transparent Content, Disableable

The panel sends a "heartbeat" to `stats.wp-panel.org` every 24 hours, but the content is only:

```json
{
  "anonymous_id": "a1b2c3d4e5f67890",  // First 16 bytes of /etc/machine-id SHA256
  "version": "1.0.0"                     // Panel version number
}
```

**Does NOT include**: IP address, domains, site count, passwords, any business data.

**Disableable**: Turn off "Anonymous Statistics" in panel Security Settings, or set `telemetry_enabled` to `false` in the database.

### 7.2 Alert Notifications: Sent Only to You

Panel alerts (high CPU, SSL expiry, backup failure, etc.) are only sent to the email or Webhook that **you actively configured**. If you haven't configured SMTP or Webhook, alerts are only logged in the local database and are **not sent externally**.

### 7.3 Update Check: Only Accesses GitHub

When checking for updates, the panel only accesses `api.github.com` to fetch Release information. It **does not download** any update files unless you **manually click "Update Now"** in the panel.

---

## 8. What You Can Verify

If you're still concerned, you can audit it yourself using the following methods:

### 8.1 Check the Panel's Network Connections

```bash
# View network connections established by the wp-panel process
ss -tpn | grep wp-panel

# Or view real-time network activity
lsof -i -a -c wp-panel
```

Under normal conditions you should only see:
- Local HTTPS listening on port 8443
- Occasional connection to `api.github.com` (checking for updates)
- If telemetry is enabled, once daily to `stats.wp-panel.org`

**Will NOT see**: connections to unknown IPs, large data uploads, persistent abnormal connections.

### 8.2 Check System Cron Jobs

```bash
# View cron jobs created by the panel
cat /etc/cron.d/wp_panel_cron

# View system-level scheduled tasks
crontab -l
ls /etc/cron.d/
```

The panel only writes to the cron file when you **actively create a scheduled task**; all content is fully transparent and readable.

### 8.3 Verify Whether the Binary Has Been Tampered With

```bash
# Calculate the current panel's SHA256
sha256sum /usr/local/bin/wp-panel

# Compare with the checksum on GitHub Releases
# https://github.com/naibabiji/wp-panel/releases
```

### 8.4 View Panel Operation Logs

The panel's **background task queue** (backups, SSL renewal, firewall bans, scheduled task execution, etc.) is recorded in the `operation_logs` table, which you can view under "Panel Settings → Operation Logs". Additionally, login attempts, system alerts, etc. have their own independent log tables.

### 8.5 Disable Telemetry

```bash
# Open the SQLite database
sqlite3 /www/server/panel/panel.db

# Disable telemetry
UPDATE security_settings SET svalue = 'false' WHERE skey = 'telemetry_enabled';
.quit

# Restart the panel
systemctl restart wp-panel
```

---

## 9. Conclusion

| Concern | Fact |
|------|------|
| Does the panel secretly change passwords? | ❌ **No**. Passwords can only be changed via the panel settings page (requires knowing the current password) or server CLI (requires root). There is no auto-password-change mechanism. |
| Can auto-update plant a trojan? | ❌ **Impossible**. Updates require SHA256 + Ed25519 signature + preflight triple verification; the source must be GitHub Releases. |
| Is it safe when server credentials haven't leaked? | ✅ **Very safe**. Six-layer defense-in-depth: concealment + dual authentication + Session/CSRF + HTTPS + operation isolation + firewall. |
| Does the panel secretly exfiltrate data? | ❌ **No**. Telemetry contains only an anonymous ID + version number, and is disableable; no other hidden network behavior. |

WP Panel's security design follows one core principle: **Defense in Depth**. There is no single line of defense, but rather multiple layers stacked — the attacker must successively break through the random path, browser detection, BasicAuth, Web login, Session, CSRF, path isolation, command whitelist, firewall... Each layer is extremely difficult to bypass; combined, they create an extraordinarily high security barrier.

More importantly, all code is **open source and auditable**. Any claim that "the panel is insecure" should point to a specific line of code, a specific function, or a specific network connection. Vague statements like "it feels unsafe" hold no weight against verifiable source code.

---

*This article is based on the Go source code in the WP Panel open-source repository. All quoted code snippets and file paths are publicly verifiable.*
