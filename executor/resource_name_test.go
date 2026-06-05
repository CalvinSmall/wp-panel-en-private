package executor

import "testing"

func TestBuildSiteNameAvoidsSeparatorCollisions(t *testing.T) {
	first := buildSiteName("ab.com")
	second := buildSiteName("a-b.com")
	if first == second {
		t.Fatalf("expected distinct resource names, got %q", first)
	}
}

func TestBuildSiteNameNormalizesEquivalentDomains(t *testing.T) {
	first := buildSiteName("Example.COM")
	second := buildSiteName(" example.com. ")
	if first != second {
		t.Fatalf("expected normalized domains to match, got %q and %q", first, second)
	}
}

func TestBuildSiteNameFitsSystemUserLimit(t *testing.T) {
	name := buildSiteName("this-is-a-very-long-domain-name-that-should-be-truncated.example.com")
	if len(name) > 27 {
		t.Fatalf("site name %q length = %d, want <= 27", name, len(name))
	}
	if len("php_"+name) > 32 {
		t.Fatalf("php user name length = %d, want <= 32", len("php_"+name))
	}
}
