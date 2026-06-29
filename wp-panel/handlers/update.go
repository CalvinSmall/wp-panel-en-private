package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/naibabiji/wp-panel/config"
	"github.com/naibabiji/wp-panel/database"
	"github.com/naibabiji/wp-panel/executor"
	"github.com/naibabiji/wp-panel/models"

	"github.com/gin-gonic/gin"
)

type UpdateHandler struct {
	CurrentVersion string
	ConfigPath     string
	Config         *config.Config
}

func getGithubProxy() string {
	var v string
	database.GetDB().QueryRow("SELECT svalue FROM security_settings WHERE skey = 'github_proxy'").Scan(&v)
	return v
}

func (h *UpdateHandler) Check(c *gin.Context) {
	latest, err := executor.FetchLatestPanelRelease(getGithubProxy())
	if err != nil {
		c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
			"current_version": h.CurrentVersion,
			"latest_version":  "",
			"has_update":      false,
			"error":           "Failed to get version info",
		}))
		return
	}

	hasUpdate := executor.CompareVersions(latest.TagName, h.CurrentVersion) > 0
	notes := latest.Body
	if idx := strings.Index(notes, "**Full Changelog**"); idx >= 0 {
		notes = strings.TrimSpace(notes[:idx])
	}
	if notes == "" {
		notes = "(No release notes)"
	}

	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"current_version": h.CurrentVersion,
		"latest_version":  latest.TagName,
		"release_notes":   notes,
		"has_update":      hasUpdate,
	}))
}

func (h *UpdateHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, models.SuccessResponse(executor.SnapshotPanelUpdateStatus()))
}

func (h *UpdateHandler) Update(c *gin.Context) {
	latest, err := executor.ExecutePanelUpdate(executor.PanelUpdateOptions{
		Trigger:        "manual",
		CurrentVersion: h.CurrentVersion,
		Proxy:          getGithubProxy(),
		ConfigPath:     h.ConfigPath,
		Config:         h.Config,
		UseWatchdog:    true,
	})
	if err != nil {
		code := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already in progress") {
			code = http.StatusConflict
		} else if strings.Contains(err.Error(), "already up to date") || strings.Contains(err.Error(), "only Linux") {
			code = http.StatusBadRequest
		}
		c.JSON(code, models.ErrorResponse(err.Error()))
		return
	}
	c.JSON(http.StatusOK, models.SuccessResponse(gin.H{
		"message": fmt.Sprintf("Updating to %s, panel will restart and run health check...", latest.TagName),
	}))
}
