package executor

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/naibabiji/wp-panel/config"
	"github.com/naibabiji/wp-panel/database"
	"github.com/naibabiji/wp-panel/models"
)

const nginxCustomDir = "/www/server/panel/nginx-custom"

func executeSaveNginxCustom(task *Task) TaskResult {
	payload, ok := task.Payload.(*SaveNginxCustomPayload)
	if !ok {
		return TaskResult{Success: false, Message: "task parameter type error"}
	}

	site := payload.Site
	domain := site.Domain

	if err := os.MkdirAll(nginxCustomDir, 0755); err != nil {
		log.Printf("failed to create config directory: %v", err)
		return TaskResult{Success: false, Message: "failed to create config directory"}
	}

	prePath := filepath.Join(nginxCustomDir, domain+".pre.conf")
	mainPath := filepath.Join(nginxCustomDir, domain+".conf")

	oldPre, _ := os.ReadFile(prePath)
	oldMain, _ := os.ReadFile(mainPath)

	if err := os.WriteFile(prePath, []byte(payload.PreContent), 0644); err != nil {
		log.Printf("failed to write pre.conf: %v", err)
		return TaskResult{Success: false, Message: "failed to write pre.conf"}
	}
	if err := os.WriteFile(mainPath, []byte(payload.Content), 0644); err != nil {
		os.WriteFile(prePath, oldPre, 0644)
		log.Printf("failed to write conf: %v", err)
		return TaskResult{Success: false, Message: "failed to write conf"}
	}

	ngxTest := exec.Command("nginx", "-t")
	out, err := ngxTest.CombinedOutput()
	if err != nil {
		os.WriteFile(prePath, oldPre, 0644)
		os.WriteFile(mainPath, oldMain, 0644)
		return TaskResult{Success: false, Message: "Nginx syntax check failed:\n" + string(out)}
	}

	exec.Command("nginx", "-s", "reload").Run()

	return TaskResult{Success: true, Message: "Nginx custom config saved and applied"}
}

func executeSetAccessLogMode(task *Task) TaskResult {
	payload, ok := task.Payload.(*SetAccessLogModePayload)
	if !ok {
		return TaskResult{Success: false, Message: "task parameter type error"}
	}

	site := payload.Site
	cfg := config.AppConfig

	engine := NewTemplateEngine(cfg.Panel.BackupDir)
	nginxData, err := nginxDataFromSiteChecked(site)
	if err != nil {
		return taskFailure("CDN Real IP config is invalid", err)
	}
	nginxData.AccessLogMode = payload.Mode

	nginxConfig, err := engine.RenderNginxConfig(nginxData)
	if err != nil {
		log.Printf("render Nginx config failed: %v", err)
		return taskFailure("render Nginx config failed", err)
	}

	if err := engine.ApplyNginxConfig(nginxConfig, site.NginxConfPath,
		nginxEnabledPath(cfg, site.NginxConfPath, site.Domain)); err != nil {
		log.Printf("apply Nginx config failed: %v", err)
		return taskFailure("apply Nginx config failed", err)
	}

	// Update database
	db := database.GetDB()
	db.Exec("UPDATE websites SET access_log_mode = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", payload.Mode, site.ID)

	// Clear log file when turning off
	if payload.Mode == "off" {
		logFile := filepath.Join(site.LogDir, "access.log")
		os.WriteFile(logFile, []byte{}, 0644)
	}

	modeLabels := map[string]string{
		"off":        "access log disabled",
		"error_only": "access log set to error-only logging",
		"full":       "access log set to full logging",
	}
	msg := modeLabels[payload.Mode]
	if msg == "" {
		msg = "access log mode updated"
	}
	return TaskResult{Success: true, Message: msg}
}

func executeSetCDNRealIP(task *Task) TaskResult {
	payload, ok := task.Payload.(*SetCDNRealIPPayload)
	if !ok {
		return TaskResult{Success: false, Message: "task parameter type error"}
	}
	site := payload.Site
	if site == nil {
		return TaskResult{Success: false, Message: "site does not exist"}
	}

	var groups []models.CDNRealIPGroup
	var err error
	if payload.Enabled {
		groups, err = GetEnabledCDNRealIPGroupsByIDs(payload.GroupIDs)
		if err != nil {
			return TaskResult{Success: false, Message: err.Error()}
		}
		if len(groups) == 0 {
			return TaskResult{Success: false, Message: "at least one config group must be selected when enabling CDN Real IP"}
		}
	}

	siteCopy := *site
	siteCopy.CDNRealIPEnabled = payload.Enabled
	siteCopy.CDNRealIPGroups = groups
	if _, err := ResolveCDNRealIPRuntime(&siteCopy); err != nil {
		return TaskResult{Success: false, Message: err.Error()}
	}

	cfg := config.AppConfig
	engine := NewTemplateEngine(cfg.Panel.BackupDir)
	nginxData, err := nginxDataFromSiteChecked(&siteCopy)
	if err != nil {
		return TaskResult{Success: false, Message: err.Error()}
	}
	nginxConfig, err := engine.RenderNginxConfig(nginxData)
	if err != nil {
		log.Printf("render Nginx config failed: %v", err)
		return taskFailure("render Nginx config failed", err)
	}

	oldEnabled := site.CDNRealIPEnabled
	oldGroupIDs := cdnRealIPGroupIDs(site.CDNRealIPGroups)
	oldNginxData, oldDataErr := nginxDataFromSiteChecked(site)
	var oldNginxConfig string
	var oldRenderErr error
	if oldDataErr == nil {
		oldNginxConfig, oldRenderErr = engine.RenderNginxConfig(oldNginxData)
	} else {
		oldRenderErr = oldDataErr
	}
	if err := SaveWebsiteCDNRealIPSettings(site.ID, payload.Enabled, payload.GroupIDs); err != nil {
		return taskFailure("failed to save CDN Real IP settings", err)
	}
	if err := engine.ApplyNginxConfig(nginxConfig, site.NginxConfPath,
		nginxEnabledPath(cfg, site.NginxConfPath, site.Domain)); err != nil {
		log.Printf("apply Nginx config failed: %v", err)
		_ = SaveWebsiteCDNRealIPSettings(site.ID, oldEnabled, oldGroupIDs)
		return taskFailure("apply Nginx config failed", err)
	}
	if err := ApplyFail2banSettings(); err != nil {
		_ = SaveWebsiteCDNRealIPSettings(site.ID, oldEnabled, oldGroupIDs)
		if oldRenderErr == nil {
			_ = engine.ApplyNginxConfig(oldNginxConfig, site.NginxConfPath,
				nginxEnabledPath(cfg, site.NginxConfPath, site.Domain))
		}
		return taskFailure("CDN Real IP rolled back; Fail2ban whitelist application failed", err)
	}

	return TaskResult{Success: true, Message: "CDN Real IP settings saved and applied"}
}

func cdnRealIPGroupIDs(groups []models.CDNRealIPGroup) []int {
	ids := make([]int, 0, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	return ids
}

func boolToDBInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func executeSetDocumentRoot(task *Task) TaskResult {
	payload, ok := task.Payload.(*SetDocumentRootPayload)
	if !ok {
		return TaskResult{Success: false, Message: "task parameter type error"}
	}
	site := payload.Site
	if site == nil {
		return TaskResult{Success: false, Message: "site does not exist"}
	}
	if site.SiteType != "php" {
		return TaskResult{Success: false, Message: "only generic PHP sites support changing the web entry directory"}
	}

	documentRootSubdir, err := NormalizeDocumentRootSubdir(site.SiteType, payload.DocumentRootSubdir)
	if err != nil {
		return TaskResult{Success: false, Message: err.Error()}
	}
	if _, err := EnsureEffectiveDocumentRoot(site.WebRoot, site.SiteType, documentRootSubdir, site.SystemUser); err != nil {
		return taskFailure("failed to prepare web entry directory", err)
	}

	siteCopy := *site
	siteCopy.DocumentRootSubdir = documentRootSubdir
	nginxData, err := nginxDataFromSiteChecked(&siteCopy)
	if err != nil {
		return taskFailure("CDN Real IP config is invalid", err)
	}

	cfg := config.AppConfig
	engine := NewTemplateEngine(cfg.Panel.BackupDir)
	nginxConfig, err := engine.RenderNginxConfig(nginxData)
	if err != nil {
		log.Printf("render Nginx config failed: %v", err)
		return taskFailure("render Nginx config failed", err)
	}
	if err := engine.ApplyNginxConfig(nginxConfig, site.NginxConfPath,
		nginxEnabledPath(cfg, site.NginxConfPath, site.Domain)); err != nil {
		log.Printf("apply Nginx config failed: %v", err)
		return taskFailure("apply Nginx config failed", err)
	}

	db := database.GetDB()
	if _, err := db.Exec("UPDATE websites SET document_root_subdir = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", documentRootSubdir, site.ID); err != nil {
		return TaskResult{Success: false, Message: "failed to save web entry directory: " + err.Error()}
	}

	if documentRootSubdir == "" {
		return TaskResult{Success: true, Message: "web entry directory switched to project root"}
	}
	return TaskResult{Success: true, Message: "web entry directory switched to public"}
}
