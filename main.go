package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/naibabiji/wp-panel/collector"
	"github.com/naibabiji/wp-panel/config"
	"github.com/naibabiji/wp-panel/database"
	"github.com/naibabiji/wp-panel/executor"
	"github.com/naibabiji/wp-panel/middleware"
	"github.com/naibabiji/wp-panel/router"

	"golang.org/x/crypto/bcrypt"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "/www/server/panel/config.json", "Config file path")
	resetPass := flag.String("passwd", "", "Reset admin password (8+ characters)")
	resetAdmin := flag.Bool("reset-admin", false, "One-click reset admin account and password")
	refreshWhitelist := flag.Bool("refresh-whitelist", false, "Manually trigger whitelist refresh")
	unbanAll := flag.Bool("unban-all", false, "One-click clear all IP ban records")
	banIPNginx := flag.String("banip-nginx", "", "Add specified IP to Nginx blacklist")
	unbanIPNginx := flag.String("unbanip-nginx", "", "Remove specified IP from Nginx blacklist")
	recordFail2banIP := flag.String("record-fail2ban", "", "Record Fail2ban ban IP")
	banJail := flag.String("ban-jail", "", "Fail2ban jail name")
	fileBackup := flag.String("file-backup", "", "Execute file backup: siteID:mode")
	runAutoBackup := flag.Bool("run-auto-backup", false, "Manually trigger auto backup (for testing)")
	showInfo := flag.Bool("info", false, "View panel info")
	updateWatchdog := flag.String("update-watchdog", "", "Internal use: panel update health check watchdog")
	flag.Parse()

	if *banIPNginx != "" || *unbanIPNginx != "" || *recordFail2banIP != "" {
		handleFail2banCLI(*configPath, *banIPNginx, *unbanIPNginx, *recordFail2banIP, *banJail)
		return
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *showInfo {
		fmt.Println("WP Panel Panel Info")
		fmt.Println("─────────────────")
		if BuildTime != "" && BuildTime != "unknown" {
			displayTime := BuildTime
			if bt, err := time.Parse(time.RFC3339, BuildTime); err == nil {
				tz := getSysTimezone()
				if loc, err := time.LoadLocation(tz); err == nil {
					displayTime = bt.In(loc).Format("2006-01-02 15:04:05")
				} else {
					displayTime = bt.Local().Format("2006-01-02 15:04:05")
				}
			}
			fmt.Printf("Version: %s (built: %s)\n", Version, displayTime)
		} else {
			fmt.Printf("Version: %s\n", Version)
		}
		fmt.Printf("HTTPS port: %d\n", cfg.Panel.TLSPort)
		fmt.Printf("Secure entry: /%s\n", cfg.Panel.RandomSuffix)
		fmt.Printf("Data directory: %s\n", cfg.Panel.DataDir)
		fmt.Printf("Config file: %s\n", *configPath)
		return
	}

	if err := database.Open(cfg.SQLite.Path); err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	if *updateWatchdog != "" {
		executor.RunUpdateWatchdog(cfg, *updateWatchdog)
		return
	}

	if err := database.RunMigrations(); err != nil {
		log.Fatalf("Database migration failed: %v", err)
	}
	// Update plugin package first, ensuring subsequent migrations copy the latest version
	executor.EnsureCacheHelperPlugin(PluginFS)
	executor.AutoDeployPluginUpdates(PluginFS)
	if err := database.RunUpgrades(); err != nil {
		log.Fatalf("Database upgrade failed: %v", err)
	}
	executor.FinalizePendingPanelUpdate(cfg, Version)

	if *resetAdmin {
		resetAllAdmin(cfg, *configPath)
		return
	}

	if *resetPass != "" {
		resetAdminPassword(cfg, *resetPass)
		return
	}

	if *refreshWhitelist {
		executor.InitQueue(cfg)
		log.Printf("Whitelist refresh result: %s", executor.RunWhitelistRefresh())
		return
	}

	if *unbanAll {
		fmt.Println(executor.UnbanAllIPs())
		return
	}

	if *fileBackup != "" {
		parts := strings.SplitN(*fileBackup, ":", 3)
		if len(parts) >= 2 {
			siteID, _ := strconv.Atoi(parts[0])
			keepCount := 3
			if len(parts) >= 3 {
				keepCount, _ = strconv.Atoi(parts[2])
			}
			if keepCount <= 0 {
				keepCount = 3
			}
			msg, err := executor.ExecuteFileBackup(siteID, parts[1], keepCount)
			if err != nil {
				log.Printf("File backup failed: %v", err)
				os.Exit(1)
			}
			log.Println(msg)
		}
		return
	}

	if *runAutoBackup {
		executor.RunAutoBackup()
		return
	}

	seedAdminUser(cfg)

	log.Println("Database initialization complete")

	executor.InitQueue(cfg)
	log.Println("Task queue initialization complete")

	collector.Start()

	executor.ApplyFail2banSettings()
	executor.EnsureOperationLogRetention()
	if err := executor.ApplyRateLimitSettings(); err != nil {
		log.Printf("Nginx rate limiting config skipped: %v", err)
	}
	if err := executor.EnsureLogMap(); err != nil {
		log.Printf("Nginx log map config skipped: %v", err)
	}
	executor.EnsureAllSiteLogrotateConfigs()
	if err := executor.EnsureNginxBannedIPsConfig(); err != nil {
		log.Printf("Nginx blacklist initialization failed: %v", err)
	}
	if err := executor.EnsureCloudflareRealIPConfig(); err != nil {
		log.Printf("Cloudflare Real IP config skipped: %v", err)
	} else if err := executor.ApplyFail2banSettings(); err != nil {
		log.Printf("Cloudflare Real IP whitelist application skipped: %v", err)
	}
	executor.EnsureFastCGICacheConfig()
	// WordPress safety baseline (idempotent, only writes if not present)
	executor.EnsureWordPressBaseline()
	// Rebuild all Nginx and PHP-FPM configs after upgrade to ensure new template rules apply to old sites
	executor.GoSafe(func() {
		if err := executor.RegenerateAllSitesNginx(); err != nil {
			log.Printf("Nginx batch rebuild partially failed: %v", err)
		}
	})
	executor.GoSafe(func() {
		if err := executor.RegenerateAllSitesFPM(); err != nil {
			log.Printf("PHP-FPM batch rebuild partially failed: %v", err)
		}
	})
	log.Println("Nginx log map config ready")
	log.Println("FastCGI cache config ready")
	log.Println("Fail2ban configuration initialized")
	executor.EnsureWPCommand()
	// Remote backup password authentication requires sshpass; startup path only prompts, does not auto-modify server software state.
	if _, err := exec.LookPath("sshpass"); err != nil {
		log.Println("sshpass is not installed, remote backup password authentication is unavailable; please install manually via install script or package manager")
	}
	executor.StartProcessGuard()
	executor.StartAlertMonitor(Version)
	executor.StartTelemetry(Version)
	executor.StartPanelAutoUpdateScheduler(Version, *configPath, cfg)
	log.Println("WordPress config baseline ensured")
	log.Println("Process guard started")
	log.Println("Alert monitor started")
	executor.StartAutoBackupScheduler()
	log.Println("Auto backup scheduler started")
	executor.StartDBBackupScheduler()
	log.Println("Panel database backup scheduler started")
	executor.StartSSLRenewalScheduler()
	log.Println("SSL auto-renewal scheduler started")
	go func() {
		for {
			time.Sleep(30 * time.Minute)
			middleware.GlobalSessionStore.CleanExpired()
		}
	}()

	r := router.SetupRouter(cfg, TemplatesFS, StaticFS, Version, *configPath)

	if cfg.Panel.TLSPort > 0 && cfg.Panel.TLSCertPath != "" && cfg.Panel.TLSKeyPath != "" {
		go func() {
			addr := fmt.Sprintf(":%d", cfg.Panel.TLSPort)
			log.Printf("WP Panel started on port %d (HTTPS)", cfg.Panel.TLSPort)
			if err := r.RunTLS(addr, cfg.Panel.TLSCertPath, cfg.Panel.TLSKeyPath); err != nil {
				log.Fatalf("HTTPS server startup failed: %v", err)
			}
		}()
	} else {
		go func() {
			addr := fmt.Sprintf(":%d", cfg.Panel.Port)
			log.Printf("WP Panel started on port %d (HTTP, TLS not configured)", cfg.Panel.Port)
			if err := r.Run(addr); err != nil {
				log.Fatalf("HTTP server startup failed: %v", err)
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down panel...")
}

func handleFail2banCLI(configPath, banIP, unbanIP, recordIP, jail string) {
	if banIP != "" {
		if err := executor.AddNginxBan(banIP); err != nil {
			log.Fatalf("Nginx ban failed: %v", err)
		}
	}
	if unbanIP != "" {
		if err := executor.RemoveNginxBan(unbanIP); err != nil {
			log.Fatalf("Nginx Unban failed: %v", err)
		}
	}
	if recordIP == "" {
		return
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	if err := database.Open(cfg.SQLite.Path); err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()
	if err := database.RunMigrations(); err != nil {
		log.Fatalf("Database migration failed: %v", err)
	}
	if err := executor.RecordFail2banBan(recordIP, jail); err != nil {
		log.Fatalf("Fail2ban ban record failed: %v", err)
	}
}

func seedAdminUser(cfg *config.Config) {
	db := database.GetDB()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	if count > 0 {
		return
	}

	_, err := db.Exec(
		"INSERT INTO admin_users (username, password_hash) VALUES (?, ?)",
		cfg.Admin.Username, cfg.Admin.PasswordHash,
	)
	if err != nil {
		log.Printf("Failed to create admin user: %v", err)
		return
	}
	log.Println("Admin user initialized from config.json")
}

func resetAllAdmin(cfg *config.Config, configPath string) {
	username := "wpadmin"
	webPass := randomString(16)
	basicPass := randomString(16)

	webHash, err := bcrypt.GenerateFromPassword([]byte(webPass), 12)
	if err != nil {
		fmt.Printf("Error: password encryption failed: %v\n", err)
		os.Exit(1)
	}
	basicHash, err := bcrypt.GenerateFromPassword([]byte(basicPass), 12)
	if err != nil {
		fmt.Printf("Error: password encryption failed: %v\n", err)
		os.Exit(1)
	}

	// Update SQLite (Web login)
	db := database.GetDB()
	var count int
	db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	if count == 0 {
		_, err = db.Exec("INSERT INTO admin_users (username, password_hash) VALUES (?, ?)", username, string(webHash))
	} else {
		_, err = db.Exec("UPDATE admin_users SET username = ?, password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1",
			username, string(webHash))
	}
	if err != nil {
		fmt.Printf("Error: database update failed: %v\n", err)
		os.Exit(1)
	}

	// Update config.json (BasicAuth)
	data, err := os.ReadFile(configPath)
	if err == nil {
		var cfgMap map[string]map[string]interface{}
		if json.Unmarshal(data, &cfgMap) == nil {
			if ba, ok := cfgMap["basic_auth"]; ok {
				ba["username"] = username
				ba["password_hash"] = string(basicHash)
			}
			if admin, ok := cfgMap["admin"]; ok {
				admin["username"] = username
				admin["password_hash"] = string(webHash)
			}
			if newData, err := json.MarshalIndent(cfgMap, "", "  "); err == nil {
				if err := os.WriteFile(configPath, newData, 0600); err != nil {
					fmt.Printf("Error: Failed to write config file: %v\n", err)
					fmt.Println("BasicAuth password not updated, please check config file permissions")
					os.Exit(1)
				}
			}
		}
	}

	fmt.Println("")
	fmt.Println("═══ Admin account reset ═══")
	fmt.Println("")
	fmt.Println(" BasicAuth  and panel  Web Login username unified to  wpadmin")
	fmt.Println("")
	fmt.Println("BasicAuth authentication (browser popup, first layer of random entry):")
	fmt.Printf("  Password: %s\n", basicPass)
	fmt.Println("")
	fmt.Println("Panel Web Login (page form, BasicAuth  after passing): ")
	fmt.Printf("  Password: %s\n", webPass)
	fmt.Println("")
	fmt.Println("⚠  After logging in, please change your password in \"Panel Settings\"")
	fmt.Println("═══ ═══════════════════ ═══")
	fmt.Println("")
	fmt.Println("Restarting panel...")
	exec.Command("systemctl", "restart", "wp-panel").Run()
}

func resetAdminPassword(cfg *config.Config, newPass string) {
	if len(newPass) < 8 {
		fmt.Println("Error: password must be at least 8 characters")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), 12)
	if err != nil {
		fmt.Printf("Error: password encryption failed: %v\n", err)
		os.Exit(1)
	}

	db := database.GetDB()

	var count int
	db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)

	if count == 0 {
		_, err = db.Exec(
			"INSERT INTO admin_users (username, password_hash) VALUES (?, ?)",
			cfg.Admin.Username, string(hash),
		)
	} else {
		_, err = db.Exec(
			"UPDATE admin_users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = 1",
			string(hash),
		)
	}

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Admin password has been reset\n")
	fmt.Printf("  Username: %s\n", cfg.Admin.Username)
	fmt.Printf("  New password: %s\n", newPass)
}

func randomString(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, n)
	for i := range result {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[idx.Int64()]
	}
	return string(result)
}

func getSysTimezone() string {
	out, _ := exec.Command("bash", "-c", "timedatectl show --property=Timezone --value 2>/dev/null").CombinedOutput()
	tz := strings.TrimSpace(string(out))
	if tz == "" {
		if data, err := os.ReadFile("/etc/timezone"); err == nil {
			tz = strings.TrimSpace(string(data))
		}
	}
	if tz == "" {
		return "UTC"
	}
	return tz
}
