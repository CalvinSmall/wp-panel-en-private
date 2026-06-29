package executor

import (
	"os"
	"strings"
)

type PHPRuntimeConfig struct {
	MemoryLimit       string
	UploadMaxFilesize string
	PostMaxSize       string
	MaxExecutionTime  string
	MaxInputTime      string
	MaxInputVars      string
}

var phpRuntimeConfigPath = "/etc/php/8.3/fpm/conf.d/99-wppanel.ini"

var phpRuntimeDefaults = PHPRuntimeConfig{
	MemoryLimit:       "256M",
	UploadMaxFilesize: "64M",
	PostMaxSize:       "64M",
	MaxExecutionTime:  "300",
	MaxInputTime:      "300",
	MaxInputVars:      "2000",
}

func PHPRuntimeConfigPath() string {
	return phpRuntimeConfigPath
}

func EnsurePHPRuntimeConfigFile() (bool, error) {
	data, err := os.ReadFile(phpRuntimeConfigPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	if os.IsNotExist(err) {
		return true, os.WriteFile(phpRuntimeConfigPath, []byte(defaultPHPRuntimeConfigContent()), 0644)
	}

	content := string(data)
	next := ensureIniValue(content, "memory_limit", phpRuntimeDefaults.MemoryLimit)
	next = ensureIniValue(next, "upload_max_filesize", phpRuntimeDefaults.UploadMaxFilesize)
	next = ensureIniValue(next, "post_max_size", phpRuntimeDefaults.PostMaxSize)
	next = ensureIniValue(next, "max_execution_time", phpRuntimeDefaults.MaxExecutionTime)
	next = ensureIniValue(next, "max_input_time", phpRuntimeDefaults.MaxInputTime)
	next = ensureIniValue(next, "max_input_vars", phpRuntimeDefaults.MaxInputVars)

	if next == content {
		return false, nil
	}
	return true, os.WriteFile(phpRuntimeConfigPath, []byte(next), 0644)
}

func LoadPHPRuntimeConfig() PHPRuntimeConfig {
	cfg := phpRuntimeDefaults
	data, err := os.ReadFile(phpRuntimeConfigPath)
	if err != nil {
		return cfg
	}
	content := string(data)
	if v := findIniValue(content, "memory_limit"); v != "" {
		cfg.MemoryLimit = v
	}
	if v := findIniValue(content, "upload_max_filesize"); v != "" {
		cfg.UploadMaxFilesize = v
	}
	if v := findIniValue(content, "post_max_size"); v != "" {
		cfg.PostMaxSize = v
	}
	if v := findIniValue(content, "max_execution_time"); v != "" {
		cfg.MaxExecutionTime = v
	}
	if v := findIniValue(content, "max_input_time"); v != "" {
		cfg.MaxInputTime = v
	}
	if v := findIniValue(content, "max_input_vars"); v != "" {
		cfg.MaxInputVars = v
	}
	return cfg
}

func defaultPHPRuntimeConfigContent() string {
	return `; WP Panel - WordPress runtime baseline
; These values are managed by Software Management.
memory_limit = 256M
upload_max_filesize = 64M
post_max_size = 64M
max_execution_time = 300
max_input_time = 300
max_input_vars = 2000
`
}

func ensureIniValue(content, key, value string) string {
	if findIniValue(content, key) != "" {
		return content
	}
	return strings.TrimRight(content, "\n") + "\n" + key + " = " + value + "\n"
}

func findIniValue(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+" =") || strings.HasPrefix(trimmed, key+"=") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
