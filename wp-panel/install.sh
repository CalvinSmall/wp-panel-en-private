#!/bin/bash
set -e
set -o pipefail

# ============================================================
# WP Panel Install Script — for Debian 13 (Trixie); a clean system is recommended
# Automatically selects official or China mirror sources for PHP 8.3,
# compatible with overseas and domestic VPS
# ============================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

INSTALL_DIR="/www/server/panel"
CONFIG_FILE="$INSTALL_DIR/config.json"
DB_PATH="$INSTALL_DIR/panel.db"
BIN_PATH="/usr/local/bin/wp-panel"
SERVICE_PATH="/etc/systemd/system/wp-panel.service"
PANEL_PORT=8888
MYSQL_PASS=""
GHPROXY="https://gh.wp-panel.org"
PREFER_CN=false
PHP_SOURCE_MODE="${WP_PANEL_PHP_SOURCE:-auto}"

if [[ "${WP_PANEL_PREFER_CN_MIRROR:-0}" == "1" ]] || [[ "${WP_PANEL_PREFER_CN_MIRROR:-}" == "true" ]]; then
    PREFER_CN=true
fi

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

systemctl_enable_best_effort() {
    local svc="$1"
    if ! systemctl enable "$svc"; then
        log_warn "${svc} auto-start enable failed; continuing install. You can manually check later: systemctl enable ${svc}"
    fi
}

systemctl_start_required() {
    local svc="$1"
    if ! systemctl start "$svc"; then
        journalctl -u "$svc" -n 20 --no-pager 2>/dev/null || true
        log_error "${svc} failed to start. Please troubleshoot using the logs above"
    fi
}

# ============================================================
# System Kernel Tuning (BBR+FQ, TCP buffers, connection queues, file descriptors)
# ============================================================
apply_system_tuning() {
    log_info "Applying system kernel tuning..."

    SYSCTL_FILE="/etc/sysctl.d/99-wp-panel.conf"
    CPU_CORES=$(nproc)

    cat > "$SYSCTL_FILE" << 'SYSCTLEOF'
# WP Panel — Network & kernel tuning

# ── Connection queues ──
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 8192
net.core.netdev_max_backlog = 16384

# ── TCP buffers ──
net.core.rmem_default = 262144
net.core.wmem_default = 262144
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216

# ── TIME-WAIT optimization ──
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.ip_local_port_range = 1024 65535

# ── Keepalive ──
net.ipv4.tcp_keepalive_time = 300
net.ipv4.tcp_keepalive_intvl = 30
net.ipv4.tcp_keepalive_probes = 5

# ── BBR aux parameters ──
net.ipv4.tcp_slow_start_after_idle = 0
net.ipv4.tcp_notsent_lowat = 16384

# ── Basic security ──
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_sack = 1
net.ipv4.tcp_timestamps = 1
SYSCTLEOF

    # BBR + FQ: only enable on machines with 2+ cores (BBR throughput can tank under CPU contention on single-core VPS)
    if [[ $CPU_CORES -ge 2 ]]; then
        cat >> "$SYSCTL_FILE" << 'BBREOF'

# ── BBR congestion control + FQ scheduler ──
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
BBREOF
        modprobe tcp_bbr 2>/dev/null || true
        log_info "BBR + FQ enabled (${CPU_CORES}-core CPU)"
    else
        log_info "Single-core CPU; skipping BBR (to avoid CPU contention side effects)"
    fi

    sysctl --system >/dev/null 2>&1

    # File descriptor limits
    if ! grep -q "nofile 65535" /etc/security/limits.conf 2>/dev/null; then
        cat >> /etc/security/limits.conf << 'LIMITSEOF'
* soft nofile 65535
* hard nofile 65535
LIMITSEOF
    fi

    log_info "System kernel tuning complete"
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefer-cn|--cn)
            PREFER_CN=true
            shift
            ;;
        --php-source)
            if [[ $# -lt 2 ]]; then
                log_error "--php-source requires one of: official, ustc, sjtu, or auto"
            fi
            PHP_SOURCE_MODE="$2"
            shift 2
            ;;
        --php-source=*)
            PHP_SOURCE_MODE="${1#*=}"
            shift
            ;;
        *)
            log_warn "Unknown parameter ignored: $1"
            shift
            ;;
    esac
done

# Show a friendly message on non-zero exit
trap 'e=$?; if [[ $e -ne 0 ]]; then echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; echo -e "${RED}  Installation incomplete${NC}"; echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"; echo -e "  Please screenshot the error above and send to:"; echo -e "  blog@naibabiji.com"; echo -e "  WeChat vv15_zhi"; echo ""; fi' EXIT

# ============================================================
# PHP 8.3 Source Selection (official + China mirror fallback chain)
# ============================================================

set_php_source_meta() {
    case "$1" in
        official)
            PHP_SOURCE_LABEL="Ondřej Surý Official"
            PHP_KEY_URL="https://packages.sury.org/debsuryorg-archive-keyring.deb"
            PHP_REPO_URL="https://packages.sury.org/php/"
            ;;
        ustc)
            PHP_SOURCE_LABEL="USTC PHP Sury Mirror"
            PHP_KEY_URL="https://mirrors.ustc.edu.cn/sury/debsuryorg-archive-keyring.deb"
            PHP_REPO_URL="https://mirrors.ustc.edu.cn/sury/php/"
            ;;
        sjtu)
            PHP_SOURCE_LABEL="SJTU PHP Sury Mirror"
            PHP_KEY_URL="https://mirror.sjtu.edu.cn/sury/debsuryorg-archive-keyring.deb"
            PHP_REPO_URL="https://mirror.sjtu.edu.cn/sury/php/"
            ;;
        *)
            return 1
            ;;
    esac
}

download_file() {
    local url="$1"
    local output="$2"
    local timeout="${3:-30}"

    rm -f "$output"
    if command -v curl &>/dev/null; then
        curl -fsSL --connect-timeout "$timeout" -o "$output" "$url" 2>/dev/null && [[ -s "$output" ]] && return 0
    fi
    if command -v wget &>/dev/null; then
        wget -q -T "$timeout" -O "$output" "$url" 2>/dev/null && [[ -s "$output" ]] && return 0
    fi
    rm -f "$output"
    return 1
}

apt_package_available() {
    local pkg="$1"
    local candidate=""

    candidate=$(LC_ALL=C apt-cache policy "$pkg" 2>/dev/null | awk '/Candidate:/ {print $2; exit}' || true)
    if [[ -n "$candidate" ]] && [[ "$candidate" != "(none)" ]]; then
        return 0
    fi

    LC_ALL=C apt-cache show "$pkg" >/dev/null 2>&1
}

php_package_available() {
    local pkg="$1"

    apt_package_available "$pkg"
}

set_debian_source_meta() {
    case "$1" in
        nju)
            DEBIAN_SOURCE_LABEL="NJU Debian Mirror"
            DEBIAN_REPO_URL="http://mirror.nju.edu.cn/debian"
            DEBIAN_SECURITY_URL="http://mirror.nju.edu.cn/debian-security"
            ;;
        ustc)
            DEBIAN_SOURCE_LABEL="USTC Debian Mirror"
            DEBIAN_REPO_URL="http://mirrors.ustc.edu.cn/debian"
            DEBIAN_SECURITY_URL="http://mirrors.ustc.edu.cn/debian-security"
            ;;
        tuna)
            DEBIAN_SOURCE_LABEL="Tsinghua Debian Mirror"
            DEBIAN_REPO_URL="http://mirrors.tuna.tsinghua.edu.cn/debian"
            DEBIAN_SECURITY_URL="http://mirrors.tuna.tsinghua.edu.cn/debian-security"
            ;;
        official)
            DEBIAN_SOURCE_LABEL="Debian Official"
            DEBIAN_REPO_URL="http://deb.debian.org/debian"
            DEBIAN_SECURITY_URL="http://security.debian.org/debian-security"
            ;;
        *)
            return 1
            ;;
    esac
}

backup_default_debian_sources() {
    local source_file=""

    mkdir -p /etc/apt/sources.list.d

    if [[ -f /etc/apt/sources.list.d/debian.sources ]]; then
        if [[ ! -f /etc/apt/sources.list.d/debian.sources.wp-panel.bak ]]; then
            cp /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list.d/debian.sources.wp-panel.bak
        fi
        mv /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list.d/debian.sources.wp-panel.disabled
    fi

    for source_file in /etc/apt/sources.list /etc/apt/sources.list.d/*.list; do
        [[ -f "$source_file" ]] || continue
        if [[ ! -f "${source_file}.wp-panel.bak" ]]; then
            cp "$source_file" "${source_file}.wp-panel.bak"
        fi
        sed -i -E '/^[[:space:]]*deb(-src)?[[:space:]].*(\/debian-security|\/debian([[:space:]\/]|$)|deb\.debian\.org|security\.debian\.org)/ s/^/# disabled by WP Panel: /' "$source_file"
    done
}

write_debian_sources() {
    local codename="$1"

    cat > /etc/apt/sources.list.d/wp-panel-debian.sources << DEBIANSOURCESEOF
Types: deb
URIs: ${DEBIAN_REPO_URL}
Suites: ${codename} ${codename}-updates
Components: main contrib non-free non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg

Types: deb
URIs: ${DEBIAN_SECURITY_URL}
Suites: ${codename}-security
Components: main contrib non-free non-free-firmware
Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg
DEBIANSOURCESEOF
}

debian_packages_available() {
    local packages=(ca-certificates wget curl gnupg lsb-release nginx mariadb-server redis-server)
    local pkg=""

    for pkg in "${packages[@]}"; do
        if ! apt_package_available "$pkg"; then
            log_warn "APT source missing candidate for critical package: ${pkg}"
            return 1
        fi
    done
    return 0
}

configure_debian_source() {
    local source_id="$1"
    local codename="$2"
    local apt_log="/tmp/wp-panel-debian-apt-update.log"

    set_debian_source_meta "$source_id" || return 1
    log_info "Trying Debian source: ${DEBIAN_SOURCE_LABEL}"
    write_debian_sources "$codename"

    if apt-get update > "$apt_log" 2>&1 && debian_packages_available; then
        rm -f "$apt_log"
        log_info "Debian source usable: ${DEBIAN_SOURCE_LABEL}"
        return 0
    fi

    log_warn "${DEBIAN_SOURCE_LABEL} unavailable or incomplete sync; trying next Debian source"
    if [[ -f "$apt_log" ]]; then
        tail -n 8 "$apt_log" 2>/dev/null || true
    fi
    rm -f "$apt_log"
    return 1
}

select_debian_source() {
    local codename="$1"
    local candidates=()
    local source_id=""

    if $PREFER_CN; then
        candidates=(nju ustc tuna official)
        backup_default_debian_sources
    else
        log_info "Using system default Debian APT sources"
        apt-get update
        debian_packages_available || log_error "System default APT sources missing critical packages; check /etc/apt/sources.list or /etc/apt/sources.list.d/"
        return 0
    fi

    for source_id in "${candidates[@]}"; do
        if configure_debian_source "$source_id" "$codename"; then
            if [[ "$source_id" == "official" ]]; then
                log_warn "China mirror sync may be delayed; falling back to official source"
            fi
            return 0
        fi
    done

    log_error "All Debian APT sources are unavailable. Check network, DNS, system clock, or manually configure a working mirror and retry."
}

configure_php_source() {
    local source_id="$1"
    local codename="$2"
    local keyring_file="/usr/share/keyrings/debsuryorg-archive-keyring.gpg"
    local tmp_key="/tmp/debsuryorg-archive-keyring.deb"
    local apt_log="/tmp/wp-panel-apt-update.log"

    set_php_source_meta "$source_id" || return 1
    log_info "Trying PHP source: ${PHP_SOURCE_LABEL}"

    if download_file "$PHP_KEY_URL" "$tmp_key" 20; then
        if ! dpkg -i "$tmp_key" >/dev/null 2>&1; then
            rm -f "$tmp_key"
            log_warn "${PHP_SOURCE_LABEL} GPG key installation failed"
            return 1
        fi
        rm -f "$tmp_key"
    else
        if [[ -f "$keyring_file" ]]; then
            log_warn "${PHP_SOURCE_LABEL} GPG key download failed; reusing existing local keyring"
        else
            log_warn "${PHP_SOURCE_LABEL} GPG key download failed"
            return 1
        fi
    fi

    cat > /etc/apt/sources.list.d/php.sources << PHPSOURCESEOF
Types: deb
URIs: ${PHP_REPO_URL}
Suites: ${codename}
Components: main
Signed-By: ${keyring_file}
PHPSOURCESEOF

    if apt-get update > "$apt_log" 2>&1 && \
        php_package_available php8.3-cli && \
        php_package_available php8.3-fpm; then
        rm -f "$apt_log"
        log_info "PHP source usable: ${PHP_SOURCE_LABEL}"
        return 0
    fi

    log_warn "${PHP_SOURCE_LABEL} unavailable; trying next PHP source"
    if [[ -f "$apt_log" ]]; then
        tail -n 8 "$apt_log" 2>/dev/null || true
    fi
    rm -f "$apt_log"
    return 1
}

select_php_source() {
    local codename="$1"
    local candidates=()

    case "$PHP_SOURCE_MODE" in
        auto|"")
            if $PREFER_CN; then
                candidates=(ustc sjtu official)
            else
                candidates=(official ustc sjtu)
            fi
            ;;
        official|ustc|sjtu)
            candidates=("$PHP_SOURCE_MODE")
            ;;
        *)
            log_warn "Unknown PHP source mode ${PHP_SOURCE_MODE}; falling back to auto"
            candidates=(official ustc sjtu)
            ;;
    esac

    for source_id in "${candidates[@]}"; do
        if configure_php_source "$source_id" "$codename"; then
            return 0
        fi
    done

    log_error "All PHP 8.3 sources are unavailable. Check network, DNS, certificate clock, or retry later."
}

# ============================================================
# Uninstall functions (defined early for pipe compatibility)
# ============================================================

do_uninstall() {
    echo ""
    echo -e "${BOLD}Uninstalling panel, please wait...${NC}"

    echo -e "  → Stopping panel service..."
    systemctl stop wp-panel 2>/dev/null || true
    systemctl disable wp-panel 2>/dev/null || true
    rm -f /etc/systemd/system/wp-panel.service
    systemctl daemon-reload
    echo -e "  ${GREEN}✓${NC} Panel service stopped"

    echo -e "  → Removing panel files..."
    rm -f "$BIN_PATH"
    rm -f /usr/local/bin/wp
    rm -rf "$INSTALL_DIR"
    echo -e "  ${GREEN}✓${NC} Panel files removed"

    echo -e "  → Cleaning Nginx panel configs..."
    rm -f /etc/nginx/conf.d/wppanel-ratelimit.conf
    rm -f /etc/nginx/conf.d/wppanel-botlimit.conf
    rm -f /etc/nginx/conf.d/wppanel-limit-status.conf
    rm -f /etc/nginx/conf.d/wppanel-cache.conf
    rm -f /etc/nginx/conf.d/wppanel-log.conf
    nginx -s reload 2>/dev/null || true
    echo -e "  ${GREEN}✓${NC} Nginx configs cleaned"

    echo ""
    log_info "Panel uninstalled. The following have been preserved:"
    log_info "  - /www/wwwroot (website files)"
    log_info "  - /www/wwwlogs (website logs)"
    log_info "  - /www/server/certificates (SSL certificates)"
    log_info "  - MariaDB databases"
    log_info "  - System packages (nginx/php/mariadb/redis/fail2ban)"
}

do_purge() {
    echo ""
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED}  WARNING: This will delete ALL website data and system software!${NC}"
    echo -e "${RED}  This action is irreversible. Choose carefully.${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "  Type ${BOLD}yes${NC} to confirm, or press Enter to cancel."

    confirm=""
    read -p "  > " confirm < /dev/tty 2>/dev/null || true
    if [[ "$confirm" != "yes" ]]; then
        log_info "Cancelled"
        return 0
    fi

    echo ""
    echo -e "${BOLD}Purging, please wait...${NC}"

    echo -e "  → Stopping all services..."
    systemctl stop wp-panel 2>/dev/null || true
    systemctl stop nginx 2>/dev/null || true
    systemctl stop php8.3-fpm 2>/dev/null || true
    systemctl stop mariadb 2>/dev/null || true
    systemctl stop redis-server 2>/dev/null || true
    systemctl stop fail2ban 2>/dev/null || true
    echo -e "  ${GREEN}✓${NC} Services stopped"

    echo -e "  → Cleaning site Nginx and PHP-FPM configs..."
    rm -f /etc/nginx/sites-enabled/*
    rm -f /etc/nginx/sites-available/*
    rm -f /etc/php/8.3/fpm/pool.d/*.conf
    echo -e "  ${GREEN}✓${NC} Configs cleaned"

    echo -e "  → Removing packages (may take 1–2 minutes)..."
    DEBIAN_FRONTEND=noninteractive apt-get purge -y nginx nginx-common mariadb-server mariadb-common redis-server fail2ban php8.3-* 2>/dev/null || true
    DEBIAN_FRONTEND=noninteractive apt-get autoremove -y 2>/dev/null || true
    echo -e "  ${GREEN}✓${NC} Packages removed"

    echo -e "  → Cleaning systemd configs..."
    systemctl disable wp-panel 2>/dev/null || true
    rm -f /etc/systemd/system/wp-panel.service
    for svc in nginx php8.3-fpm mariadb redis-server; do
        rm -rf "/etc/systemd/system/${svc}.service.d/wp-panel.conf"
    done
    systemctl daemon-reload
    echo -e "  ${GREEN}✓${NC} systemd cleaned"

    echo -e "  → Restoring system kernel parameters..."
    rm -f /etc/sysctl.d/99-wp-panel.conf
    sysctl --system >/dev/null 2>&1
    sed -i '/nofile 65535/d' /etc/security/limits.conf 2>/dev/null || true
    echo -e "  ${GREEN}✓${NC} Kernel parameters restored"

    echo -e "  → Removing panel files..."
    rm -f "$BIN_PATH"
    rm -f /usr/local/bin/wp
    rm -rf "$INSTALL_DIR"
    echo -e "  ${GREEN}✓${NC} Panel files removed"

    echo -e "  → Removing website data..."
    rm -rf /www/wwwroot /www/wwwlogs /www/server/certificates
    rm -f /etc/nginx/conf.d/wppanel-*.conf
    rm -rf /var/cache/nginx/fastcgi
    echo -e "  ${GREEN}✓${NC} Website data removed"

    if grep -q "/swapfile" /etc/fstab 2>/dev/null; then
        echo -e "  → Cleaning swap file..."
        swapoff /swapfile 2>/dev/null || true
        rm -f /swapfile
        sed -i '/\/swapfile/d' /etc/fstab
        echo -e "  ${GREEN}✓${NC} Swap removed"
    fi

    echo ""
    log_info "Purge complete; system restored to pre-install state"
}

# ============================================================
# Permission Check
# ============================================================
if [[ $EUID -ne 0 ]]; then
    log_error "Please run this script with root privileges"
fi
log_info "Permission check passed"

# ============================================================
# Reinstall / Remnant Detection
# ============================================================
INSTALL_COMPLETE=false
INSTALL_TRACES=false

if [[ -f "$CONFIG_FILE" ]] && [[ -s "$BIN_PATH" ]] && [[ -x "$BIN_PATH" ]]; then
    INSTALL_COMPLETE=true
fi

if [[ -e "$CONFIG_FILE" ]] || [[ -e "$BIN_PATH" ]] || [[ -d "$INSTALL_DIR" ]] || [[ -f "$SERVICE_PATH" ]]; then
    INSTALL_TRACES=true
fi

if $INSTALL_COMPLETE; then
    echo ""
    echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${YELLOW}  Detected existing WP Panel installation${NC}"
    echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "  1) Uninstall then reinstall (${GREEN}keep sites/DB/SSL/software${NC})"
    echo -e "  2) Uninstall panel only (${GREEN}keep sites/DB/SSL/software${NC})"
    echo -e "  3) Full purge (${RED}delete all data and remove software${NC})"
    echo -e "  4) Exit"
    echo ""
    echo -e "  Enter a number and press Enter to choose."

    read -p "  > " choice < /dev/tty 2>/dev/null || read choice

    case "${choice:-4}" in
        1)
            do_uninstall
            log_info "Starting reinstall..."
            ;;
        2)
            do_uninstall
            exit 0
            ;;
        3)
            do_purge
            exit 0
            ;;
        *)
            echo -e "${GREEN}Cancelled; panel remains in current state${NC}"
            exit 0
            ;;
    esac
elif $INSTALL_TRACES; then
    echo ""
    echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${YELLOW}  Detected incomplete or leftover WP Panel installation${NC}"
    echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo -e "  1) Continue / repair installation (${GREEN}recommended${NC})"
    echo -e "  2) Clean leftovers then reinstall (${GREEN}keep sites/DB/SSL/software${NC})"
    echo -e "  3) Remove panel leftovers only (${GREEN}keep sites/DB/SSL/software${NC})"
    echo -e "  4) Full purge (${RED}delete all data and remove software${NC})"
    echo -e "  5) Exit"
    echo ""
    echo -e "  Press Enter to continue / repair installation."

    read -p "  > " choice < /dev/tty 2>/dev/null || read choice

    case "${choice:-1}" in
        1)
            log_info "Continuing / repairing installation..."
            ;;
        2)
            do_uninstall
            log_info "Starting reinstall..."
            ;;
        3)
            do_uninstall
            exit 0
            ;;
        4)
            do_purge
            exit 0
            ;;
        *)
            echo -e "${GREEN}Cancelled; system remains in current state${NC}"
            exit 0
            ;;
    esac
fi

# ============================================================
# System Detection & Swap Configuration
# ============================================================
if ! grep -qi "debian" /etc/os-release 2>/dev/null; then
    log_error "This script only supports Debian systems"
fi

TOTAL_MEM_KB=$(grep MemTotal /proc/meminfo | awk '{print $2}')
TOTAL_MEM_MB=$((TOTAL_MEM_KB / 1024))
log_info "Physical memory: ${TOTAL_MEM_MB}MB"

if [[ $TOTAL_MEM_MB -le 1024 ]]; then
    log_info "Memory <= 1GB; creating 2GB swap partition..."
    SWAP_FILE="/swapfile"
    if [[ ! -f "$SWAP_FILE" ]]; then
        dd if=/dev/zero of=$SWAP_FILE bs=1M count=2048 status=progress
        chmod 600 $SWAP_FILE
        mkswap $SWAP_FILE
        swapon $SWAP_FILE
        echo "$SWAP_FILE none swap sw 0 0" >> /etc/fstab
        log_info "Swap partition created"
    else
        log_info "Swap partition already exists; skipped"
    fi
fi

# ============================================================
# APT Source Configuration
# ============================================================
log_info "Configuring APT sources..."
export DEBIAN_FRONTEND=noninteractive
DEBIAN_CODENAME=""
if command -v lsb_release &>/dev/null; then
    DEBIAN_CODENAME=$(lsb_release -sc 2>/dev/null || true)
fi
if [[ -z "$DEBIAN_CODENAME" ]] && [[ -f /etc/os-release ]]; then
    DEBIAN_CODENAME=$(grep '^VERSION_CODENAME=' /etc/os-release 2>/dev/null | cut -d= -f2 || true)
fi
if [[ -z "$DEBIAN_CODENAME" ]]; then
    log_error "Unable to identify Debian codename"
fi
log_info "Debian version: ${DEBIAN_CODENAME}"

# China mode prioritizes Debian mirrors and covers debian-security / debian-updates.
select_debian_source "$DEBIAN_CODENAME"

# Install base dependencies
apt-get install -y curl wget unzip ca-certificates gnupg lsb-release

# PHP 8.3 source uses separate multi-fallback; China mode prioritizes USTC / SJTU mirrors.
select_php_source "$DEBIAN_CODENAME"

# ============================================================
# Install Base Components
# ============================================================
log_info "Installing system components..."

apt-get install -y \
    nginx \
    mariadb-server \
    redis-server \
    fail2ban \
    nftables \
    sshpass \
    rsyslog \
    cron \
    php8.3-fpm \
    php8.3-mysql \
    php8.3-curl \
    php8.3-gd \
    php8.3-mbstring \
    php8.3-xml \
    php8.3-zip \
    php8.3-intl \
    php8.3-redis \
    php8.3-opcache \
    php8.3-cli

log_info "Base components installed"

# ============================================================
# systemd Process Guardian Configuration
# ============================================================
log_info "Configuring systemd process guardian..."

for svc in nginx php8.3-fpm mariadb redis-server; do
    DROPDIR="/etc/systemd/system/${svc}.service.d"
    mkdir -p "$DROPDIR"
    cat > "$DROPDIR/wp-panel.conf" << SYSTEMDEOF
[Service]
Restart=always
RestartSec=5s
StartLimitIntervalSec=0
SYSTEMDEOF
done

systemctl daemon-reload
log_info "systemd process guardian configuration complete"

# ============================================================
# Nginx Base Configuration
# ============================================================
log_info "Configuring Nginx basics..."

mkdir -p /etc/nginx/conf.d

cat > /etc/nginx/conf.d/wppanel-ratelimit.conf << 'RATELIMITEOF'
# WP Panel — Request rate limiting
# Logged-in WordPress users are not rate-limited
map $http_cookie $wp_rate_limit_key {
    ~*wordpress_logged_in "";
    default $binary_remote_addr;
}

limit_req_zone $wp_rate_limit_key zone=wp_req_limit:10m rate=60r/m;
RATELIMITEOF

cat > /etc/nginx/conf.d/wppanel-limit-status.conf << 'LIMITSTATUSEOF'
# WP Panel Generated - shared limit_req status
limit_req_status 429;
LIMITSTATUSEOF

# FastCGI cache
mkdir -p /var/cache/nginx/fastcgi
cat > /etc/nginx/conf.d/wppanel-cache.conf << 'CACHEEOF'
fastcgi_cache_path /var/cache/nginx/fastcgi levels=1:2 keys_zone=WP_CACHE:200m inactive=60m max_size=2g;
CACHEEOF

nginx -t && nginx -s reload 2>/dev/null || true
log_info "Nginx base configuration complete"

# ============================================================
# Firewall — Opening Panel Port 8443
# ============================================================
log_info "Opening panel port 8443..."

# nftables
if command -v nft &>/dev/null && nft list ruleset 2>/dev/null | grep -q "hook input"; then
    nft add rule inet filter input tcp dport 8443 accept 2>/dev/null || \
    nft add rule ip filter input tcp dport 8443 accept 2>/dev/null || true
    log_info "nftables: 8443 opened"
fi

# ufw
if command -v ufw &>/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow 8443/tcp 2>/dev/null || true
    log_info "ufw: 8443 opened"
fi

# ============================================================
# MariaDB Security Hardening
# ============================================================
log_info "Configuring MariaDB..."

systemctl_start_required mariadb
systemctl_enable_best_effort mariadb

# Prefer reading existing password (to avoid inconsistency from interrupted installs)
if [[ -f "$CONFIG_FILE" ]]; then
    MYSQL_PASS=$(grep -o '"root_password": "[^"]*"' "$CONFIG_FILE" 2>/dev/null | cut -d'"' -f4 || true)
fi
if [[ -z "$MYSQL_PASS" ]]; then
    MYSQL_PASS=$(head -c 24 /dev/urandom | sha256sum | head -c 32)
fi

if mysql -u root -p"${MYSQL_PASS}" -e "SELECT 1" 2>/dev/null; then
    log_info "MariaDB root password verified"
elif mysql -u root -e "SELECT 1" 2>/dev/null; then
    mysqladmin -u root password "${MYSQL_PASS}" 2>/dev/null
    log_info "MariaDB root password set"
else
    log_warn "MariaDB password state abnormal; panel will auto-repair on first startup"
fi

mysql -u root -p"${MYSQL_PASS}" -e "
    DELETE FROM mysql.user WHERE User='';
    DELETE FROM mysql.user WHERE User='root' AND Host!='localhost';
    DROP DATABASE IF EXISTS test;
    DELETE FROM mysql.db WHERE Db='test' OR Db='test\\_%';
    FLUSH PRIVILEGES;
" 2>/dev/null || log_warn "Some hardening steps skipped (password may already be set)"

if [[ $TOTAL_MEM_MB -le 1024 ]]; then
    log_info "Low-memory environment; optimizing MariaDB config..."
    cat > /etc/mysql/mariadb.conf.d/99-wp-panel.cnf << 'MARIADBEOF'
[mysqld]
innodb_buffer_pool_size = 128M
innodb_log_buffer_size = 8M
table_open_cache = 128
max_connections = 30
performance_schema = OFF
MARIADBEOF
    systemctl restart mariadb || systemctl_start_required mariadb
fi

# ============================================================
# Directory Structure
# ============================================================
log_info "Creating directory structure..."

mkdir -p "$INSTALL_DIR"/{backups,packages,logs,certs}
mkdir -p /www/wwwroot
mkdir -p /www/wwwlogs
mkdir -p /www/server/certificates
chmod 700 "$INSTALL_DIR"

# ============================================================
# Generate Self-Signed SSL Certificate (10-year validity)
# ============================================================
log_info "Generating self-signed SSL certificate..."

CERT_DIR="$INSTALL_DIR/certs"
CERT_FILE="$CERT_DIR/panel.crt"
KEY_FILE="$CERT_DIR/panel.key"

openssl req -x509 -nodes -days 3650 -newkey rsa:2048 \
    -keyout "$KEY_FILE" \
    -out "$CERT_FILE" \
    -subj "/C=CN/ST=Shanghai/L=Shanghai/O=WP Panel/OU=IT/CN=WP-Panel-SelfSigned" \
    -addext "subjectAltName=IP:127.0.0.1" \
    2>/dev/null

chmod 600 "$KEY_FILE"
chmod 644 "$CERT_FILE"
log_info "Self-signed certificate generated (10-year validity)"

# ============================================================
# Download WordPress Package
# ============================================================
log_info "Downloading WordPress package..."
WP_ZIP="$INSTALL_DIR/packages/wordpress.zip"
WP_ZIP_TMP="${WP_ZIP}.download"
for i in 1 2 3; do
    if download_file "https://wordpress.org/latest.zip" "$WP_ZIP_TMP" 60; then
        mv "$WP_ZIP_TMP" "$WP_ZIP"
        log_info "WordPress download complete"
        break
    fi
    log_warn "Download failed, retrying ($i/3)..."
    sleep 3
done
rm -f "$WP_ZIP_TMP"
if [[ ! -s "$WP_ZIP" ]]; then
    rm -f "$WP_ZIP"
    log_warn "WordPress download failed; will download on first site creation"
fi

# ============================================================
# Generate Panel Security Credentials
# ============================================================
log_info "Generating security credentials..."

PANEL_SUFFIX=$(head -c 20 /dev/urandom | sha256sum | head -c 8)

BASIC_USER="admin"
BASIC_PASS=$(head -c 12 /dev/urandom | base64 | head -c 16)
WEB_USER="wpadmin"
WEB_PASS=$(head -c 12 /dev/urandom | base64 | head -c 16)

BASIC_HASH=""
WEB_HASH=""
if command -v php8.3 &>/dev/null; then
    BASIC_HASH=$(php8.3 -r "echo password_hash('$BASIC_PASS', PASSWORD_BCRYPT, ['cost' => 12]);" 2>/dev/null)
    WEB_HASH=$(php8.3 -r "echo password_hash('$WEB_PASS', PASSWORD_BCRYPT, ['cost' => 12]);" 2>/dev/null)
fi
if [[ -z "$BASIC_HASH" ]] && command -v python3 &>/dev/null; then
    BASIC_HASH=$(python3 -c "import bcrypt; print(bcrypt.hashpw(b'$BASIC_PASS', bcrypt.gensalt(12)).decode())" 2>/dev/null)
    WEB_HASH=$(python3 -c "import bcrypt; print(bcrypt.hashpw(b'$WEB_PASS', bcrypt.gensalt(12)).decode())" 2>/dev/null)
fi
if [[ -z "$BASIC_HASH" ]]; then
    log_warn "Unable to generate bcrypt hash; panel will auto-reset password on first startup"
    BASIC_HASH='$2a$12$00000000000000000000000000000000000000000000000000000'
    WEB_HASH='$2a$12$00000000000000000000000000000000000000000000000000000'
fi

# ============================================================
# Write config.json
# ============================================================
log_info "Writing config file..."

cat > "$CONFIG_FILE" << CONFIGEOF
{
  "panel": {
    "version": "1.0.0-mvp",
    "port": $PANEL_PORT,
    "tls_port": 8443,
    "tls_cert_path": "$CERT_FILE",
    "tls_key_path": "$KEY_FILE",
    "random_suffix": "$PANEL_SUFFIX",
    "data_dir": "$INSTALL_DIR",
    "backup_dir": "$INSTALL_DIR/backups",
    "log_dir": "$INSTALL_DIR/logs"
  },
  "sqlite": {
    "path": "$DB_PATH"
  },
  "mariadb": {
    "host": "localhost",
    "port": 3306,
    "socket": "/run/mysqld/mysqld.sock",
    "root_user": "root",
    "root_password": "$MYSQL_PASS"
  },
  "admin": {
    "username": "$WEB_USER",
    "password_hash": "$WEB_HASH"
  },
  "basic_auth": {
    "username": "$BASIC_USER",
    "password_hash": "$BASIC_HASH"
  },
  "paths": {
    "www_root": "/www/wwwroot",
    "www_logs": "/www/wwwlogs",
    "nginx_sites_available": "/etc/nginx/sites-available",
    "nginx_sites_enabled": "/etc/nginx/sites-enabled",
    "php_fpm_pool": "/etc/php/8.3/fpm/pool.d",
    "php_fpm_sock": "/run/php",
    "certificates": "/www/server/certificates",
    "wordpress_package": "$INSTALL_DIR/packages/wordpress.zip",
    "cron_file": "/etc/cron.d/wp_panel_cron"
  },
  "security": {
    "basic_auth_enabled": true,
    "max_login_attempts": 5,
    "attempt_window_minutes": 5,
    "ban_duration_hours": 24,
    "auto_whitelist_enabled": true,
    "core_ports": [22, $PANEL_PORT, 80, 443, 8443]
  },
  "systemd": {
    "service_name": "wp-panel",
    "service_path": "$SERVICE_PATH",
    "binary_path": "$BIN_PATH"
  }
}
CONFIGEOF

chmod 600 "$CONFIG_FILE"

# ============================================================
# Deploy Go Binary
# ============================================================
log_info "Deploying panel binary..."

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GITHUB_RELEASE="https://github.com/naibabiji/wp-panel/releases/latest/download/wp-panel"
GHPROXY_RELEASE="${GHPROXY}/${GITHUB_RELEASE}"

install_downloaded_binary() {
    local url="$1"
    local label="$2"
    local tmp_bin="/tmp/wp-panel.$$.download"

    log_info "Attempting to download panel binary: ${label}"
    if download_file "$url" "$tmp_bin" 60; then
        chmod +x "$tmp_bin"
        mv "$tmp_bin" "$BIN_PATH"
        log_info "Panel binary download complete: ${label}"
        return 0
    fi
    rm -f "$tmp_bin"
    log_warn "${label} download failed"
    return 1
}

if [[ -s "$SCRIPT_DIR/wp-panel" ]]; then
    cp "$SCRIPT_DIR/wp-panel" "$BIN_PATH"
    chmod +x "$BIN_PATH"
    log_info "Panel binary deployed (local file)"
else
    DOWNLOAD_OK=false
    if $PREFER_CN; then
        install_downloaded_binary "$GHPROXY_RELEASE" "gh.wp-panel.org proxy" && DOWNLOAD_OK=true
        if ! $DOWNLOAD_OK; then
            install_downloaded_binary "$GITHUB_RELEASE" "GitHub Releases direct" && DOWNLOAD_OK=true
        fi
    else
        install_downloaded_binary "$GITHUB_RELEASE" "GitHub Releases direct" && DOWNLOAD_OK=true
        if ! $DOWNLOAD_OK; then
            install_downloaded_binary "$GHPROXY_RELEASE" "gh.wp-panel.org proxy" && DOWNLOAD_OK=true
        fi
    fi

    if ! $DOWNLOAD_OK; then
        log_error "Unable to fetch release binary. Solutions:
  1. Check whether your server can reach GitHub Releases or gh.wp-panel.org
  2. Manually download the release artifact wp-panel, place it in the same directory as install.sh, and re-run
  3. Or compile locally and upload: go build -o wp-panel ."
    fi
fi

# ============================================================
# Create systemd Service
# ============================================================
log_info "Creating systemd service..."

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
StandardOutput=journal
StandardError=journal
SyslogIdentifier=wp-panel
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
SYSTEMDEOF

systemctl daemon-reload
systemctl_enable_best_effort wp-panel
systemctl_start_required wp-panel

apply_system_tuning

# ============================================================
# Port Listening Check
# ============================================================
PORT_OK=false
if systemctl is-active --quiet wp-panel; then
    sleep 3
    for i in 1 2 3 4 5 6 7 8; do
        if ss -tlnp 2>/dev/null | grep -q ":8443"; then
            PORT_OK=true
            break
        fi
        sleep 2
    done
fi

# ============================================================
# Final Output
# ============================================================
if systemctl is-active --quiet wp-panel; then
    STATUS="${GREEN}Running${NC}"
else
    STATUS="${RED}Not running${NC}"
fi

LOCAL_IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[[ -z "$LOCAL_IP" ]] && LOCAL_IP="<Unknown>"

PUBLIC_IP=$(curl -s --connect-timeout 5 ip.sb 2>/dev/null || curl -s --connect-timeout 5 ifconfig.me 2>/dev/null)
[[ -z "$PUBLIC_IP" ]] && PUBLIC_IP="<Unknown>"

echo ""
echo -e "${BOLD}============================================${NC}"
echo -e "${BOLD}  WP Panel Installation Complete${NC}"
echo -e "${BOLD}============================================${NC}"
echo ""
echo -e "${BOLD}Official Sources:${NC}"
echo -e "  Website:     ${BOLD}https://wp-panel.org${NC}"
echo -e "  GitHub:     ${BOLD}https://github.com/naibabiji/wp-panel${NC}"
echo -e "  Aside from wp-panel.org and this GitHub repo, no other domain is affiliated with this project."
echo ""
echo -e "Public IP:   ${BOLD}${PUBLIC_IP}${NC}"
echo -e "Local IP:    ${BOLD}${LOCAL_IP}${NC}"
echo ""
if [[ "$PUBLIC_IP" != "<Unknown>" ]]; then
    echo -e "Panel URL:   ${BOLD}https://${PUBLIC_IP}:8443/${PANEL_SUFFIX}/${NC}"
    if [[ "$LOCAL_IP" != "<Unknown>" && "$LOCAL_IP" != "$PUBLIC_IP" ]]; then
        echo -e "LAN URL:     ${BOLD}https://${LOCAL_IP}:8443/${PANEL_SUFFIX}/${NC}"
    fi
else
    echo -e "Panel URL:   ${BOLD}https://${LOCAL_IP}:8443/${PANEL_SUFFIX}/${NC}"
fi
echo -e "Panel Status: ${STATUS}"
if $PORT_OK; then
    echo -e "Port:        ${GREEN}8443 listening${NC}"
else
    echo -e "Port:        ${YELLOW}8443 not listening; check logs: journalctl -u wp-panel -n 20${NC}"
fi
echo ""
echo -e "  ┌─────────────────────────────────────────┐"
echo -e "  │  Layer 1 — BasicAuth (browser prompt)       │"
echo -e "  ├─────────────────────────────────────────┤"
echo -e "  │  Username: ${BOLD}${BASIC_USER}${NC}"
echo -e "  │  Password: ${BOLD}${BASIC_PASS}${NC}"
echo -e "  └─────────────────────────────────────────┘"
echo ""
echo -e "  ┌─────────────────────────────────────────┐"
echo -e "  │  Layer 2 — Web Login (panel form)           │"
echo -e "  ├─────────────────────────────────────────┤"
echo -e "  │  Username: ${BOLD}${WEB_USER}${NC}"
echo -e "  │  Password: ${BOLD}${WEB_PASS}${NC}"
echo -e "  └─────────────────────────────────────────┘"
echo ""
echo -e "  ${BOLD}Login Flow:${NC}"
echo -e "  1. Open the panel URL in your browser → BasicAuth dialog appears"
echo -e "     → Enter ${BOLD}Layer 1${NC} username and password"
echo -e "  2. After passing, you see the login page → Enter ${BOLD}Layer 2${NC} username and password"
echo -e "  3. Enter the console"
echo ""
echo -e "${YELLOW}⚠ A self-signed certificate is in use; browsers will show a security warning${NC}"
echo -e "${YELLOW}  Click \"Advanced\" → \"Proceed to site\" to access the panel${NC}"
echo -e "${YELLOW}  The panel uses port 8443 (HTTPS) and does not conflict with Nginx site port 443${NC}"
echo ""
echo -e "${BOLD}Cannot access?${NC}"
echo -e "  1. On cloud servers, check that ${YELLOW}security group / firewall${NC} allows port 8443"
echo -e "  2. Check local firewall: ${BOLD}nft list ruleset${NC}"
echo -e "  3. View panel logs: ${BOLD}journalctl -u wp-panel -f${NC}"
echo ""
echo -e "${BOLD}Software Paths:${NC}"
echo -e "  Nginx:      /etc/nginx/"
echo -e "  PHP-FPM:    /etc/php/8.3/fpm/"
echo -e "  MariaDB:    /etc/mysql/"
echo -e "  Redis:      /etc/redis/"
echo -e "  Panel binary: /usr/local/bin/wp-panel"
echo -e "  Panel data:   /www/server/panel/"
echo -e "  SSL certs:    ${CERT_DIR}/"
echo ""
echo -e "${BOLD}Panel CLI (wp):${NC}"
echo -e "  wp              Show panel info"
echo -e "  wp restart      Restart panel"
echo -e "  wp password     One-click reset admin password"
echo -e "  wp unban        One-click clear all IP bans"
echo -e "  wp status       Show runtime status"
echo ""
echo -e "${YELLOW}Save these credentials immediately — they are shown only once${NC}"
echo ""
echo -e "${BOLD}Anonymous Install Statistics${NC}"
echo -e "  The panel reports anonymous install stats once daily, containing only:"
echo -e "  Anonymous machine ID (SHA256 hash of /etc/machine-id)"
echo -e "  Panel version number"
echo -e "  No IP, domain, site info, or other sensitive data is reported."
echo -e "  To disable, turn it off in panel security settings."
echo ""
