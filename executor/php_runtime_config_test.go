package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsurePHPRuntimeConfigFileAddsMissingKeys(t *testing.T) {
	oldPath := phpRuntimeConfigPath
	phpRuntimeConfigPath = filepath.Join(t.TempDir(), "99-wppanel.ini")
	t.Cleanup(func() { phpRuntimeConfigPath = oldPath })

	original := "memory_limit = 512M\nupload_max_filesize = 128M\npost_max_size = 128M\nmax_execution_time = 100\nmax_input_vars = 3000\n"
	if err := os.WriteFile(phpRuntimeConfigPath, []byte(original), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	changed, err := EnsurePHPRuntimeConfigFile()
	if err != nil {
		t.Fatalf("ensure config: %v", err)
	}
	if !changed {
		t.Fatal("expected missing max_input_time to be added")
	}

	data, err := os.ReadFile(phpRuntimeConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "max_input_time = 300") {
		t.Fatalf("expected max_input_time default, got:\n%s", content)
	}
	if !strings.Contains(content, "upload_max_filesize = 128M") {
		t.Fatalf("expected existing values to be preserved, got:\n%s", content)
	}
}

func TestRenderPHPFPMPoolUsesRuntimeConfig(t *testing.T) {
	oldPath := phpRuntimeConfigPath
	phpRuntimeConfigPath = filepath.Join(t.TempDir(), "99-wppanel.ini")
	t.Cleanup(func() { phpRuntimeConfigPath = oldPath })

	content := "memory_limit = 512M\nupload_max_filesize = 128M\npost_max_size = 128M\nmax_execution_time = 100\nmax_input_time = 100\nmax_input_vars = 3000\n"
	if err := os.WriteFile(phpRuntimeConfigPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	engine := NewTemplateEngine("")
	rendered, err := engine.RenderPHPFPMPool(&PHPFPMPoolData{
		Domain:     "example.com",
		PoolName:   "example_com",
		SystemUser: "wp_example",
		WebRoot:    "/www/wwwroot/example.com",
		SocketPath: "/run/php",
		SocketName: "example_com",
	})
	if err != nil {
		t.Fatalf("render pool: %v", err)
	}

	for _, want := range []string{
		"php_admin_value[upload_max_filesize] = 128M",
		"php_admin_value[post_max_size] = 128M",
		"php_admin_value[max_execution_time] = 100",
		"php_admin_value[max_input_time] = 100",
		"php_admin_value[memory_limit] = 512M",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered pool missing %q:\n%s", want, rendered)
		}
	}
}
