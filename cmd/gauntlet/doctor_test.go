package main

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pmclSF/gauntlet/internal/fixture"
	"github.com/pmclSF/gauntlet/internal/proxy"
	"github.com/pmclSF/gauntlet/internal/runner"
)

func TestRunDoctor_FailsRecordedModeWithoutReplayLockfile(t *testing.T) {
	restore := stubDoctorRuntime(t, runner.EgressOpen)
	defer restore()

	root := t.TempDir()
	evalsDir := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evalsDir, "smoke")
	fixturesDir := filepath.Join(evalsDir, "fixtures")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite: %v", err)
	}
	if err := os.MkdirAll(fixturesDir, 0o755); err != nil {
		t.Fatalf("mkdir fixtures: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte("scenario: doctor_lock\ninput:\n  messages:\n    - role: user\n      content: hi\n"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	configPath := filepath.Join(evalsDir, "gauntlet.yml")
	mustWriteDoctorPolicy(t, configPath, "recorded")

	caDir := filepath.Join(evalsDir, ".gauntlet")
	if _, err := proxy.GenerateCA(caDir); err != nil {
		t.Fatalf("generate ca: %v", err)
	}

	store := fixture.NewStore(fixturesDir)
	if err := store.EnableFixtureSigning(filepath.Join(caDir, "fixture-signing-key.pem")); err != nil {
		t.Fatalf("enable fixture signing: %v", err)
	}

	report, err := runDoctor(doctorOptions{
		Suite:        "smoke",
		ConfigPath:   configPath,
		StrictPolicy: true,
	})
	if err == nil {
		t.Fatal("expected doctor failure when replay lockfile is missing")
	}
	if report.Passed {
		t.Fatal("expected report to fail")
	}
	if !hasDoctorCheck(report, "fixtures", doctorFail) {
		t.Fatalf("expected failing fixtures check, got %#v", report.Checks)
	}
}

func TestRunDoctor_PassesPassthroughWithoutReplayAssets(t *testing.T) {
	restore := stubDoctorRuntime(t, runner.EgressOpen)
	defer restore()

	root := t.TempDir()
	evalsDir := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evalsDir, "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte("scenario: doctor_passthrough\ninput:\n  messages:\n    - role: user\n      content: hi\n"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	configPath := filepath.Join(evalsDir, "gauntlet.yml")
	mustWriteDoctorPolicy(t, configPath, "passthrough")

	report, err := runDoctor(doctorOptions{
		Suite:        "smoke",
		ConfigPath:   configPath,
		StrictPolicy: true,
	})
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if !report.Passed {
		t.Fatalf("expected doctor pass report, got %#v", report.Checks)
	}
	if !hasDoctorCheck(report, "fixtures", doctorPass) {
		t.Fatalf("expected fixture skip/pass check, got %#v", report.Checks)
	}
}

func TestRunDoctor_WarnsWhenCARotationDueSoon(t *testing.T) {
	restore := stubDoctorRuntime(t, runner.EgressOpen)
	defer restore()

	now := time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC)
	doctorNowFn = func() time.Time { return now }
	doctorLoadCAFn = func(string) (*proxy.CA, error) {
		return &proxy.CA{
			Cert: &x509.Certificate{
				NotAfter: now.Add(7 * 24 * time.Hour),
			},
		}, nil
	}

	root := t.TempDir()
	evalsDir := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evalsDir, "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte("scenario: doctor_rotation\ninput:\n  messages:\n    - role: user\n      content: hi\n"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	configPath := filepath.Join(evalsDir, "gauntlet.yml")
	mustWriteDoctorPolicy(t, configPath, "live")

	report, err := runDoctor(doctorOptions{
		Suite:        "smoke",
		ConfigPath:   configPath,
		StrictPolicy: true,
	})
	if err != nil {
		t.Fatalf("runDoctor: %v", err)
	}
	if !report.Passed {
		t.Fatalf("expected pass with warning, got checks=%#v", report.Checks)
	}
	if !hasDoctorCheck(report, "cert_trust", doctorWarn) {
		t.Fatalf("expected cert_trust warning, got %#v", report.Checks)
	}
}

func TestRunDoctor_FailsOnInsecureCAPermissions(t *testing.T) {
	restore := stubDoctorRuntime(t, runner.EgressOpen)
	defer restore()

	root := t.TempDir()
	evalsDir := filepath.Join(root, "evals")
	suiteDir := filepath.Join(evalsDir, "smoke")
	if err := os.MkdirAll(suiteDir, 0o755); err != nil {
		t.Fatalf("mkdir suite: %v", err)
	}
	if err := os.WriteFile(filepath.Join(suiteDir, "scenario.yaml"), []byte("scenario: doctor_ca_perms\ninput:\n  messages:\n    - role: user\n      content: hi\n"), 0o644); err != nil {
		t.Fatalf("write scenario: %v", err)
	}

	configPath := filepath.Join(evalsDir, "gauntlet.yml")
	mustWriteDoctorPolicy(t, configPath, "live")

	caDir := filepath.Join(evalsDir, ".gauntlet")
	if _, err := proxy.GenerateCA(caDir); err != nil {
		t.Fatalf("generate ca: %v", err)
	}
	if err := os.Chmod(filepath.Join(caDir, "ca.key"), 0o644); err != nil {
		t.Fatalf("chmod ca.key: %v", err)
	}

	report, err := runDoctor(doctorOptions{
		Suite:        "smoke",
		ConfigPath:   configPath,
		StrictPolicy: true,
	})
	if err == nil {
		t.Fatal("expected doctor failure for insecure CA key permissions")
	}
	if report.Passed {
		t.Fatalf("expected report failure, got %#v", report.Checks)
	}
	if !hasDoctorCheck(report, "cert_trust", doctorFail) {
		t.Fatalf("expected failing cert_trust check, got %#v", report.Checks)
	}
	found := false
	for _, check := range report.Checks {
		if check.Name == "cert_trust" && strings.Contains(strings.ToLower(check.Detail), "permissions") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected permission context in cert_trust detail, got %#v", report.Checks)
	}
}

func stubDoctorRuntime(t *testing.T, egress runner.EgressStatus) func() {
	t.Helper()
	oldEgress := doctorCheckEgressFn
	oldLoad := doctorLoadCAFn
	oldProxyCheck := doctorProxyBindCheckFn
	oldNow := doctorNowFn
	oldIdentity := doctorTrustedIdentityFrom
	doctorCheckEgressFn = func() runner.EgressStatus { return egress }
	doctorLoadCAFn = proxy.LoadCA
	doctorProxyBindCheckFn = func(string) error { return nil }
	doctorNowFn = func() time.Time { return time.Now().UTC() }
	doctorTrustedIdentityFrom = func() []string { return nil }
	return func() {
		doctorCheckEgressFn = oldEgress
		doctorLoadCAFn = oldLoad
		doctorProxyBindCheckFn = oldProxyCheck
		doctorNowFn = oldNow
		doctorTrustedIdentityFrom = oldIdentity
	}
}

func mustWriteDoctorPolicy(t *testing.T, configPath, modelMode string) {
	t.Helper()
	content := "version: 1\n" +
		"suites:\n" +
		"  smoke:\n" +
		"    scenarios: \"evals/smoke/*.yaml\"\n" +
		"    runner_mode: local\n" +
		"    model_mode: " + modelMode + "\n" +
		"proxy:\n" +
		"  addr: \"127.0.0.1:0\"\n" +
		"tut:\n" +
		"  command: \"python3\"\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
}

func hasDoctorCheck(report *doctorReport, name string, status doctorStatus) bool {
	for _, check := range report.Checks {
		if check.Name == name && check.Status == status {
			return true
		}
	}
	return false
}
