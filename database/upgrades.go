// Package database — version upgrade mechanism
//
// Division of roles between the two files:
//
//   - migrations.go   Full schema creation + seed data, used for fresh installs.
//                      Always represents the latest database state.
//                      Runs in full on every startup (idempotent via IF NOT EXISTS / OR IGNORE).
//   - upgrades.go     Incremental upgrade steps, used for upgrading old versions.
//                      Only executed sequentially when the version is behind.
//
// Database change workflow:
//
//   1. Add CREATE / INSERT statements to the corresponding section in migrations.go (for fresh installs).
//   2. Append an Upgrade entry to the end of upgrades.go (for upgrades).
//   3. Upgrade entries are permanent and must never be deleted. Users may upgrade across
//      multiple versions; deleting upgrade entries would cause old versions to skip
//      necessary incremental migrations such as ALTER TABLE.
//
// Runtime logic (main.go startup → database.Open → RunMigrations → RunUpgrades):
//
//   Fresh install:  migrations create all tables + seed → upgrades detect empty version table → skip all → write latest version
//   Upgrade:        migrations run idempotently (no actual change) → upgrades detect version lag → execute missing upgrades sequentially → update version
//   Already latest: migrations run idempotently → upgrades detect version is latest → skip
//
// Version convention:
//   Uses semantic versioning (e.g. "1.0.0"), matching Git tags. LatestVersion() returns the version
//   of the last entry in the upgrades list ("1.0.0" if the list is empty), i.e. the database version
//   that the current code represents.

package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// Upgrade defines the database changes for a single version upgrade.
// SQL statements should use IF NOT EXISTS / OR IGNORE for idempotency.
// Func is an optional Go code migration, executed after SQL, for non-DB operations such as filesystem cleanup.
type Upgrade struct {
	Version     string       // Target version, e.g. "1.0.0"
	Description string       // What this upgrade does
	SQL         []string     // SQL statements to execute
	Func        func() error // Optional Go migration function
}

// registeredFuncs holds upgrade functions registered by external packages,
// solving the circular dependency problem (database cannot import executor).
var registeredFuncs = map[string]func() error{}

// RegisterUpgrade allows external packages to register an upgrade function.
// The version must match a Version in the upgrades list.
func RegisterUpgrade(version string, fn func() error) {
	registeredFuncs[version] = fn
}

// upgrades are ordered by version (old → new), permanently retained.
// Never delete old entries — cross-version upgrades depend on the complete migration chain.
// The history was cleared once at the v1.0.0 stable release; all upgrade entries accumulate from then on.
var upgrades = []Upgrade{
	{
		Version:     "1.0.1",
		Description: "Migrate wp-panel-config.json out of web root and rotate API keys",
		Func:        migratePluginConfigs,
	},
	{
		Version:     "1.0.2",
		Description: "Add per-site XML-RPC toggle, disabled by default",
		SQL: []string{
			`ALTER TABLE websites ADD COLUMN xmlrpc_enabled INTEGER NOT NULL DEFAULT 0`,
		},
	},
	{
		Version:     "1.0.3",
		Description: "Add running column to cron_jobs and seed Redis Cache as a default plugin",
		SQL: []string{
			`ALTER TABLE cron_jobs ADD COLUMN running INTEGER NOT NULL DEFAULT 0`,
			`INSERT OR IGNORE INTO wp_extension_config (etype, slug, name, enabled) VALUES ('plugin', 'redis-cache', 'Redis Cache', 1)`,
		},
	},
	{
		Version:     "1.0.4",
		Description: "Strengthen per-site Unix user group isolation and sensitive file permissions",
	},
	{
		Version:     "1.0.5",
		Description: "Add system update alert toggle",
		SQL: []string{
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('alert_system_update', 'true', 'System updates available alert')`,
		},
	},
	{
		Version:     "1.0.6",
		Description: "Add panel version alert toggle",
		SQL: []string{
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('alert_panel_update', 'true', 'Panel new version alert')`,
		},
	},
	{
		Version:     "1.0.7",
		Description: "Add WP_DEBUG / post revisions / memory limit optimization settings",
		SQL: []string{
			`ALTER TABLE websites ADD COLUMN wp_debug_enabled INTEGER NOT NULL DEFAULT 0`,
			`ALTER TABLE websites ADD COLUMN wp_post_revisions INTEGER NOT NULL DEFAULT -1`,
			`ALTER TABLE websites ADD COLUMN wp_memory_limit TEXT NOT NULL DEFAULT ''`,
		},
	},
	{
		Version:     "1.0.8",
		Description: "Add anonymous install analytics toggle",
		SQL: []string{
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('telemetry_enabled', 'true', 'Anonymous install analytics (reports only machine ID and version)')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('telemetry_url', '', 'Custom analytics report URL (leave empty to use default)')`,
		},
	},
	{
		Version:     "1.0.9",
		Description: "Add GitHub proxy address setting",
		SQL: []string{
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('github_proxy', '', 'GitHub proxy address; leave empty for direct connection')`,
		},
	},
	{
		Version:     "1.0.10",
		Description: "Backfill WP_CACHE_KEY_SALT for existing WordPress sites",
	},
	{
		Version:     "1.0.11",
		Description: "Add WordPress security log path allowlist setting",
		SQL: []string{
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('wp_security_log_whitelist', '', 'WordPress security log path allowlist')`,
		},
	},
	{
		Version:     "1.0.12",
		Description: "Add per-site CDN real IP configuration groups",
		SQL: []string{
			`CREATE TABLE IF NOT EXISTS cdn_realip_groups (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				name        TEXT    NOT NULL UNIQUE,
				provider    TEXT    NOT NULL DEFAULT 'custom',
				header_name TEXT    NOT NULL,
				ip_ranges   TEXT    NOT NULL DEFAULT '',
				builtin     INTEGER NOT NULL DEFAULT 0,
				enabled     INTEGER NOT NULL DEFAULT 1,
				description TEXT    DEFAULT '',
				created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE INDEX IF NOT EXISTS idx_cdn_realip_groups_enabled ON cdn_realip_groups(enabled)`,
			`CREATE TABLE IF NOT EXISTS website_cdn_realip_groups (
				website_id INTEGER NOT NULL,
				group_id   INTEGER NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY (website_id, group_id),
				FOREIGN KEY (website_id) REFERENCES websites(id) ON DELETE CASCADE,
				FOREIGN KEY (group_id) REFERENCES cdn_realip_groups(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_website_cdn_realip_groups_group ON website_cdn_realip_groups(group_id)`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('cloudflare_realip_ips', '', 'Cloudflare Real IP official IP ranges')`,
			`INSERT OR IGNORE INTO cdn_realip_groups (name, provider, header_name, ip_ranges, builtin, enabled, description) VALUES
				('Cloudflare', 'cloudflare', 'CF-Connecting-IP', '', 1, 1, 'Cloudflare official IP ranges auto-fetched by panel'),
				('Generic CDN (Compatibility Mode)', 'compatible', 'X-Forwarded-For', '', 1, 1, 'Trusts X-Forwarded-For directly without origin IP verification')`,
		},
		Func: ensureCDNRealIPEnabledColumn,
	},
	{
		Version:     "1.0.13",
		Description: "Add unified bot UA rate limiting settings",
		SQL: []string{
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('bot_limit_enabled', 'false', 'Enable unified bot UA rate limiting')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('bot_limit_rpm', '30', 'Max bot requests per site per minute')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('bot_limit_burst', '20', 'Bot burst allowance')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('googlebot_ips', '', 'Googlebot official IP range cache')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('bingbot_ips', '', 'Bingbot official IP range cache')`,
		},
	},
	{
		Version:     "1.0.14",
		Description: "Record last SSL application failure reason per site",
		Func:        ensureSSLLastErrorColumn,
	},
	{
		Version:     "1.0.15",
		Description: "Add panel auto-update settings",
		SQL: []string{
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_enabled', 'false', 'Enable panel auto-update')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_mode', 'patch_only', 'Panel auto-update mode: patch_only/all_stable')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_window', '03:00-05:00', 'Panel auto-update time window')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_release_delay_minutes', '15', 'Panel auto-update release delay (minutes)')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_signature_timeout_minutes', '120', 'Panel auto-update signature wait timeout (minutes)')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_last_target_version', '', 'Panel auto-update last target version')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_last_check_at', '', 'Panel auto-update last check time')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_last_attempt_at', '', 'Panel auto-update last attempt time')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_last_status', '', 'Panel auto-update last status')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_last_stage', '', 'Panel auto-update last stage')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_last_error', '', 'Panel auto-update last error')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_last_success_at', '', 'Panel auto-update last success time')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_last_success_version', '', 'Panel auto-update last success version')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_signature_wait_version', '', 'Panel auto-update signature wait version')`,
			`INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('panel_auto_update_signature_wait_at', '', 'Panel auto-update signature wait start time')`,
		},
	},
	{
		Version:     "1.0.16",
		Description: "Add per-site SSL certificate export toggle",
		Func:        ensureSSLExportEnabledColumn,
	},
	{
		Version:     "1.0.17",
		Description: "Add per-site PHP web document root subdirectory config",
		Func:        ensureDocumentRootSubdirColumn,
	},
	{
		Version:     "1.0.18",
		Description: "Add per-site AI read-only diagnosis settings and session recording",
		SQL: []string{
			`CREATE TABLE IF NOT EXISTS ai_settings (
				id              INTEGER PRIMARY KEY,
				enabled         INTEGER NOT NULL DEFAULT 0,
				provider        TEXT    NOT NULL DEFAULT 'deepseek',
				base_url        TEXT    NOT NULL DEFAULT 'https://api.deepseek.com',
				model           TEXT    NOT NULL DEFAULT 'deepseek-v4-pro',
				api_key         TEXT    NOT NULL DEFAULT '',
				timeout_seconds INTEGER NOT NULL DEFAULT 60,
				created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`INSERT OR IGNORE INTO ai_settings (id) VALUES (1)`,
			`CREATE TABLE IF NOT EXISTS ai_sessions (
				id             INTEGER PRIMARY KEY AUTOINCREMENT,
				site_id        INTEGER NOT NULL,
				symptom        TEXT    NOT NULL DEFAULT '',
				status         TEXT    NOT NULL DEFAULT 'pending',
				risk_level     TEXT    NOT NULL DEFAULT '',
				summary        TEXT    NOT NULL DEFAULT '',
				report_json    TEXT    NOT NULL DEFAULT '',
				raw_text       TEXT    NOT NULL DEFAULT '',
				prompt_chars   INTEGER NOT NULL DEFAULT 0,
				response_chars INTEGER NOT NULL DEFAULT 0,
				error_message  TEXT    NOT NULL DEFAULT '',
				created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (site_id) REFERENCES websites(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_sessions_site ON ai_sessions(site_id, created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_sessions_status ON ai_sessions(site_id, status)`,
		},
	},
	{
		Version:     "1.0.19",
		Description: "Add AI diagnosis session follow-up message recording",
		SQL: []string{
			`CREATE TABLE IF NOT EXISTS ai_messages (
				id             INTEGER PRIMARY KEY AUTOINCREMENT,
				session_id     INTEGER NOT NULL,
				role           TEXT    NOT NULL DEFAULT '',
				content        TEXT    NOT NULL DEFAULT '',
				prompt_chars   INTEGER NOT NULL DEFAULT 0,
				response_chars INTEGER NOT NULL DEFAULT 0,
				error_message  TEXT    NOT NULL DEFAULT '',
				created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (session_id) REFERENCES ai_sessions(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_messages_session ON ai_messages(session_id, created_at)`,
		},
	},
	{
		Version:     "1.0.20",
		Description: "Add S3-compatible object storage backend for remote backups",
		SQL: []string{
			`CREATE TABLE IF NOT EXISTS remote_backup_settings (
				id          INTEGER PRIMARY KEY AUTOINCREMENT,
				enabled     INTEGER NOT NULL DEFAULT 0,
				backup_type TEXT    NOT NULL DEFAULT 'rsync',
				host        TEXT    NOT NULL DEFAULT '',
				port        INTEGER NOT NULL DEFAULT 22,
				username    TEXT    NOT NULL DEFAULT 'root',
				auth_type   TEXT    NOT NULL DEFAULT 'password',
				password    TEXT    NOT NULL DEFAULT '',
				ssh_key     TEXT    NOT NULL DEFAULT '',
				remote_path TEXT    NOT NULL DEFAULT '',
				keep_local  INTEGER NOT NULL DEFAULT 1,
				s3_endpoint      TEXT NOT NULL DEFAULT '',
				s3_bucket        TEXT NOT NULL DEFAULT '',
				s3_region        TEXT NOT NULL DEFAULT 'auto',
				s3_access_key_id TEXT NOT NULL DEFAULT '',
				s3_secret_key    TEXT NOT NULL DEFAULT '',
				s3_path_prefix   TEXT NOT NULL DEFAULT '',
				created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`INSERT OR IGNORE INTO remote_backup_settings (id) VALUES (1)`,
			`ALTER TABLE remote_backup_settings ADD COLUMN backup_type TEXT NOT NULL DEFAULT 'rsync'`,
			`ALTER TABLE remote_backup_settings ADD COLUMN s3_endpoint TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE remote_backup_settings ADD COLUMN s3_bucket TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE remote_backup_settings ADD COLUMN s3_region TEXT NOT NULL DEFAULT 'auto'`,
			`ALTER TABLE remote_backup_settings ADD COLUMN s3_access_key_id TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE remote_backup_settings ADD COLUMN s3_secret_key TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE remote_backup_settings ADD COLUMN s3_path_prefix TEXT NOT NULL DEFAULT ''`,
		},
	},
}

func ensureDocumentRootSubdirColumn() error {
	var tableExists int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'websites'`).Scan(&tableExists); err != nil {
		return err
	}
	if tableExists == 0 {
		return nil
	}
	var exists int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('websites') WHERE name = 'document_root_subdir'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 {
		return nil
	}
	_, err := DB.Exec(`ALTER TABLE websites ADD COLUMN document_root_subdir TEXT NOT NULL DEFAULT ''`)
	return err
}

func ensureSSLLastErrorColumn() error {
	var tableExists int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'websites'`).Scan(&tableExists); err != nil {
		return err
	}
	if tableExists == 0 {
		return nil
	}
	var exists int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('websites') WHERE name = 'ssl_last_error'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 {
		return nil
	}
	_, err := DB.Exec(`ALTER TABLE websites ADD COLUMN ssl_last_error TEXT NOT NULL DEFAULT ''`)
	return err
}

func ensureCDNRealIPEnabledColumn() error {
	var exists int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('websites') WHERE name = 'cdn_realip_enabled'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 {
		return nil
	}
	_, err := DB.Exec(`ALTER TABLE websites ADD COLUMN cdn_realip_enabled INTEGER NOT NULL DEFAULT 0`)
	return err
}

func ensureSSLExportEnabledColumn() error {
	var tableExists int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'websites'`).Scan(&tableExists); err != nil {
		return err
	}
	if tableExists == 0 {
		return nil
	}
	var exists int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('websites') WHERE name = 'ssl_export_enabled'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 1 {
		return nil
	}
	_, err := DB.Exec(`ALTER TABLE websites ADD COLUMN ssl_export_enabled INTEGER NOT NULL DEFAULT 0`)
	return err
}

// LatestVersion returns the latest version number in the upgrades list.
func LatestVersion() string {
	if len(upgrades) == 0 {
		return "1.0.0"
	}
	return upgrades[len(upgrades)-1].Version
}

// newInstallCanary extracts the table and column name from the last ALTER TABLE ADD COLUMN
// in the upgrades list, used as a canary column to detect whether the database already has
// the latest schema (fresh install detection).
func newInstallCanary() (table, column string) {
	for i := len(upgrades) - 1; i >= 0; i-- {
		for _, sql := range upgrades[i].SQL {
			upper := strings.ToUpper(strings.TrimSpace(sql))
			if strings.HasPrefix(upper, "ALTER TABLE") && strings.Contains(upper, "ADD COLUMN") {
				fields := strings.Fields(sql)
				// ALTER TABLE <table> ADD COLUMN <column> ...
				for j, f := range fields {
					if strings.ToUpper(f) == "TABLE" && j+1 < len(fields) {
						table = fields[j+1]
					}
					if strings.ToUpper(f) == "COLUMN" && j+1 < len(fields) {
						column = fields[j+1]
						if idx := strings.Index(column, "("); idx > 0 {
							column = column[:idx]
						}
					}
				}
				if table != "" && column != "" {
					return
				}
			}
		}
	}
	return "", ""
}

func isBetaVersion(v string) bool {
	return strings.Contains(strings.ToLower(v), "beta")
}

// RunUpgrades executes all version upgrades that have not yet been applied.
// A fresh install database is already at the latest version and skips all upgrades.
func RunUpgrades() error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	// Ensure the version tracking table exists
	if _, err := DB.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version    TEXT NOT NULL,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Query current version
	var currentVersion string
	if err := DB.QueryRow("SELECT version FROM schema_version ORDER BY updated_at DESC, rowid DESC LIMIT 1").Scan(&currentVersion); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to query current version: %w", err)
	}

	// Fresh install detection: when currentVersion is empty, check whether the database
	// already has the latest schema. migrations.go has already created all tables;
	// if the canary column from the latest upgrade already exists, this is a fresh install
	// and no upgrades need to be executed.
	if currentVersion == "" {
		if table, col := newInstallCanary(); col != "" {
			var exists int
			if err := DB.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, col).Scan(&exists); err != nil {
				return fmt.Errorf("failed to inspect database schema: %w", err)
			}
			if exists > 0 {
				log.Printf("[upgrade] Fresh install database, skipping all upgrade steps")
				if _, err := DB.Exec("INSERT INTO schema_version (version) VALUES (?)", LatestVersion()); err != nil {
					return fmt.Errorf("failed to record fresh install version: %w", err)
				}
				return nil
			}
		}
	}

	// Normalize beta versions to the 1.0.0 stable baseline
	if currentVersion != "" && isBetaVersion(currentVersion) {
		log.Printf("[upgrade] normalizing beta version %s to 1.0.0", currentVersion)
		if _, err := DB.Exec("DELETE FROM schema_version"); err != nil {
			log.Printf("[upgrade] failed to clean up beta version records: %v", err)
		} else if _, err := DB.Exec("INSERT INTO schema_version (version) VALUES ('1.0.0')"); err != nil {
			log.Printf("[upgrade] failed to write normalized version: %v", err)
		} else {
			currentVersion = "1.0.0"
		}
	}

	// Validate current version: must be in the upgrades list, baseline 1.0.0, or empty (fresh install)
	if currentVersion != "" && currentVersion != "1.0.0" && currentVersion != LatestVersion() {
		found := false
		for _, u := range upgrades {
			if u.Version == currentVersion {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown database version %s, please manually migrate to 1.0.0 first", currentVersion)
		}
	}

	// Baseline 1.0.0 is treated as having all old upgrades applied; start from the first entry
	applied := currentVersion == "" || currentVersion == "1.0.0"

	for _, u := range upgrades {
		if !applied {
			if u.Version == currentVersion {
				applied = true
			}
			continue
		}

		log.Printf("[upgrade] executing %s: %s", u.Version, u.Description)

		for _, sql := range u.SQL {
			if _, err := DB.Exec(sql); err != nil {
				if strings.Contains(err.Error(), "duplicate column name") {
					log.Printf("[upgrade] %s: column already exists, skipping (%s)", u.Version, strings.TrimSpace(sql))
					continue
				}
				return fmt.Errorf("upgrade %s failed: %w\nSQL: %s", u.Version, err, sql)
			}
		}

		fn := u.Func
		if fn == nil {
			fn = registeredFuncs[u.Version]
		}
		if fn != nil {
			if err := fn(); err != nil {
				return fmt.Errorf("upgrade %s function migration failed: %w", u.Version, err)
			}
		}

		if _, err := DB.Exec("INSERT INTO schema_version (version) VALUES (?)", u.Version); err != nil {
			return fmt.Errorf("failed to record upgrade version %s: %w", u.Version, err)
		}

		log.Printf("[upgrade] %s complete", u.Version)
	}

	// Fresh install database: no version records; write the latest version directly
	// so that the next startup skips all upgrades.
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM schema_version").Scan(&count); err != nil {
		log.Printf("[upgrade] failed to query version records: %v", err)
	}
	if count == 0 {
		if _, err := DB.Exec("INSERT INTO schema_version (version) VALUES (?)", LatestVersion()); err != nil {
			return fmt.Errorf("failed to record fresh install version: %w", err)
		}
	}

	return nil
}
