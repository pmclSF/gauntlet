package fixture

import "testing"

func TestBuildProvenance_UsesEnvAndHeaders(t *testing.T) {
	t.Setenv("GAUNTLET_COMMIT_SHA", "abc123")
	t.Setenv("GAUNTLET_RECORDER_IDENTITY", "ci-bot")

	p := BuildProvenance(map[string]string{
		"User-Agent":                  "openai-python/1.1.0",
		"X-Stainless-Package-Version": "0.55.0",
	}, "proxy_live")
	if p == nil {
		t.Fatal("expected provenance")
	}
	if p.CommitSHA != "abc123" {
		t.Fatalf("CommitSHA = %q, want abc123", p.CommitSHA)
	}
	if p.RecorderIdentity != "ci-bot" {
		t.Fatalf("RecorderIdentity = %q, want ci-bot", p.RecorderIdentity)
	}
	if p.Source != "proxy_live" {
		t.Fatalf("Source = %q, want proxy_live", p.Source)
	}
	if p.ToolchainVersions["go"] == "" {
		t.Fatal("expected go toolchain version")
	}
	if p.SDKVersions["user_agent"] != "openai-python/1.1.0" {
		t.Fatalf("user_agent sdk version not captured: %+v", p.SDKVersions)
	}
	if p.SDKVersions["x_stainless_package_version"] != "0.55.0" {
		t.Fatalf("stainless sdk version not captured: %+v", p.SDKVersions)
	}
}
