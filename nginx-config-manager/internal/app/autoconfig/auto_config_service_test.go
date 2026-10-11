package autoconfig

import (
	"automation-hub-nginxconfigmanager/internal/app/config"
	"automation-hub-nginxconfigmanager/internal/app/entities"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"
)

func TestAddConfigUsesURLPathForPublicLocation(t *testing.T) {
	config.AppConfig = config.Configuration{ConfigDir: t.TempDir()}

	changed, err := addConfig(entities.Automation{
		Name:    "Dashboard",
		URLPath: "dashboard",
		Host:    "backend",
		Port:    8080,
	})
	if err != nil {
		t.Fatalf("addConfig: %v", err)
	}
	if !changed {
		t.Fatal("addConfig did not report the initial write")
	}

	raw, err := os.ReadFile(filepath.Join(config.AppConfig.ConfigDir, "dashboard.conf"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "location /dashboard/") {
		t.Fatalf("config = %q, want route based on URLPath", text)
	}
	if strings.Contains(text, "location /backend/") {
		t.Fatalf("config = %q, must not expose upstream host as public route", text)
	}
}

func TestAddConfigAtomicallyReplacesExistingConfig(t *testing.T) {
	config.AppConfig = config.Configuration{ConfigDir: t.TempDir()}
	auto := entities.Automation{
		Name:    "Dashboard",
		URLPath: "dashboard",
		Host:    "backend",
		Port:    8080,
	}
	if changed, err := addConfig(auto); err != nil || !changed {
		t.Fatalf("first addConfig: %v", err)
	}

	auto.Port = 9090
	if changed, err := addConfig(auto); err != nil || !changed {
		t.Fatalf("second addConfig: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(config.AppConfig.ConfigDir, "dashboard.conf"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "set $upstream_port 9090;") {
		t.Fatalf("config = %q, want replacement upstream", raw)
	}
	tempFiles, err := filepath.Glob(filepath.Join(config.AppConfig.ConfigDir, ".*.tmp"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(tempFiles) != 0 {
		t.Fatalf("temporary config files remain: %v", tempFiles)
	}
}

func TestRemoveConfigIsIdempotentForOutboxRedelivery(t *testing.T) {
	config.AppConfig = config.Configuration{ConfigDir: t.TempDir()}

	changed, err := removeConfig("already-removed")
	if err != nil {
		t.Fatalf("repeated removeConfig should succeed: %v", err)
	}
	if changed {
		t.Fatal("repeated removeConfig reported a filesystem change")
	}
}

func TestAddConfigSkipsIdenticalRewrite(t *testing.T) {
	config.AppConfig = config.Configuration{ConfigDir: t.TempDir()}
	auto := entities.Automation{Name: "Dashboard", URLPath: "dashboard", Host: "backend", Port: 8080}
	changed, err := addConfig(auto)
	if err != nil || !changed {
		t.Fatalf("initial addConfig = (%v, %v), want changed success", changed, err)
	}
	path := filepath.Join(config.AppConfig.ConfigDir, "dashboard.conf")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	changed, err = addConfig(auto)
	if err != nil || changed {
		t.Fatalf("replayed addConfig = (%v, %v), want unchanged success", changed, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("identical replay changed mtime: %s -> %s", before.ModTime(), after.ModTime())
	}
}

func TestConfigPathRejectsTraversal(t *testing.T) {
	config.AppConfig = config.Configuration{ConfigDir: t.TempDir()}

	if _, err := configPath("../outside"); err == nil {
		t.Fatalf("expected traversal to be rejected")
	}
}

func TestManageConfigRejectsUpdateWithoutOldPath(t *testing.T) {
	config.AppConfig = config.Configuration{ConfigDir: t.TempDir()}

	err := manageConfig(Update, entities.Automation{
		Name:    "Dashboard",
		URLPath: "dashboard",
		Host:    "backend",
		Port:    8080,
	})
	if err == nil {
		t.Fatalf("expected missing oldUrlPath to be rejected")
	}
}

func TestProcessMessageIgnoresMissingAutomationPayload(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("processMessage panicked: %v", recovered)
		}
	}()

	processMessage(&sarama.ConsumerMessage{Value: []byte(`{"type":"create"}`)})
}

func TestApplyMessageReturnsPoisonMessageErrors(t *testing.T) {
	for _, msg := range []*sarama.ConsumerMessage{
		nil,
		{Value: []byte(`not-json`)},
		{Value: []byte(`{"type":"create"}`)},
		{Value: []byte(`{"type":"unexpected","automation":{"name":"Test","urlPath":"test","host":"backend","port":80}}`)},
	} {
		if err := applyMessage(msg); err == nil {
			t.Fatalf("applyMessage(%v) unexpectedly succeeded", msg)
		}
	}
}

func TestReloadNginxSkipsWhenDisabled(t *testing.T) {
	config.AppConfig = config.Configuration{ReloadEnabled: false}

	if err := reloadNginx(); err != nil {
		t.Fatalf("reloadNginx() error = %v, want nil", err)
	}
}

func TestReloadNginxFailsClosedWithoutDockerSocketControl(t *testing.T) {
	config.AppConfig = config.Configuration{ReloadEnabled: true}

	err := reloadNginx()
	if err == nil {
		t.Fatal("reloadNginx() error = nil, want fail-closed error")
	}
	if !strings.Contains(err.Error(), "Docker socket control is disabled") {
		t.Fatalf("reloadNginx() error = %q", err)
	}
}
