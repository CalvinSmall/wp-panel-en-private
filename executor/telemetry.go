package executor

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/naibabiji/wp-panel/database"
)

const defaultTelemetryURL = "https://stats.wp-panel.org"

type heartbeatPayload struct {
	AnonymousID string `json:"anonymous_id"`
	Version     string `json:"version"`
}

var telemetryVersion string

// StartTelemetry starts anonymous statistics: a fresh panel install reports once
// immediately, then once per day around UTC 00:00.
// The payload only contains an anonymous ID (first 16 bytes of SHA-256 of machine-id)
// and the panel version number.
func StartTelemetry(version string) {
	telemetryVersion = version

	if !isTelemetryEnabled() {
		log.Println("[Telemetry] Anonymous statistics disabled, skipping")
		return
	}

	go func() {
		// New panel install (never successfully reported) reports immediately;
		// updates/restarts skip this step.
		if isFirstHeartbeat() {
			sendHeartbeat()
		}

		// Calculate time until next UTC 00:00, add ±5 minute random jitter
		now := time.Now().UTC()
		midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		jitter := time.Duration(rand.Intn(600)-300) * time.Second
		waitDur := midnight.Sub(now) + jitter
		if waitDur < 0 {
			waitDur = 0
		}

		<-time.After(waitDur)
		sendHeartbeat()

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			sendHeartbeat()
		}
	}()
}

// isFirstHeartbeat checks whether a heartbeat has never been successfully sent (fresh panel install).
func isFirstHeartbeat() bool {
	db := database.GetDB()
	if db == nil {
		return true
	}
	var val string
	err := db.QueryRow("SELECT svalue FROM security_settings WHERE skey = 'telemetry_first_sent'").Scan(&val)
	return err != nil || val == ""
}

func sendHeartbeat() {
	if !isTelemetryEnabled() {
		return
	}

	anonID := generateAnonymousID()
	if anonID == "" {
		return
	}

	url := getTelemetryURL()
	payload := heartbeatPayload{AnonymousID: anonID, Version: telemetryVersion}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url+"/api/heartbeat", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("[Telemetry] Report failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[Telemetry] Report returned unexpected status: %d", resp.StatusCode)
		return
	}

	// Mark first heartbeat as sent; subsequent restarts will not report immediately
	db := database.GetDB()
	if db != nil {
		db.Exec("INSERT OR IGNORE INTO security_settings (skey, svalue, description) VALUES ('telemetry_first_sent', ?, 'First heartbeat report time')", time.Now().UTC().Format(time.RFC3339))
	}

	log.Println("[Telemetry] Anonymous heartbeat reported successfully")
}

func generateAnonymousID() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil {
		data, err = os.ReadFile("/var/lib/dbus/machine-id")
		if err != nil {
			return ""
		}
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:16])
}

func getTelemetryURL() string {
	db := database.GetDB()
	if db == nil {
		return defaultTelemetryURL
	}
	var url string
	db.QueryRow("SELECT svalue FROM security_settings WHERE skey = 'telemetry_url'").Scan(&url)
	if url == "" {
		return defaultTelemetryURL
	}
	return url
}

func isTelemetryEnabled() bool {
	db := database.GetDB()
	if db == nil {
		return true
	}
	var val string
	db.QueryRow("SELECT svalue FROM security_settings WHERE skey = 'telemetry_enabled'").Scan(&val)
	return val != "false"
}
