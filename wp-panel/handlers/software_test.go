package handlers

import "testing"

func TestFindPHPIniValueSkipsComments(t *testing.T) {
	content := "; memory_limit = 128M\n# memory_limit = 192M\nmemory_limit = 256M\n"
	if got := findPHPIniValue(content, "memory_limit"); got != "256M" {
		t.Fatalf("findPHPIniValue() = %q, want 256M", got)
	}
}

func TestPHPConfigRequiresPoolRebuild(t *testing.T) {
	if !phpConfigRequiresPoolRebuild("post_max_size") {
		t.Fatal("post_max_size should rebuild PHP-FPM pools")
	}
	if phpConfigRequiresPoolRebuild("max_input_vars") {
		t.Fatal("max_input_vars should only reload PHP-FPM")
	}
}
