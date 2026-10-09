package autoconfig

import (
	"automation-hub-nginxconfigmanager/internal/app/config"
	"automation-hub-nginxconfigmanager/internal/app/entities"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IBM/sarama"
)

func TestAddConfigUsesURLPathForPublicLocation(t *testing.T) {
	config.AppConfig = config.Configuration{ConfigDir: t.TempDir()}

	err := addConfig(entities.Automation{
		Name:    "Dashboard",
		URLPath: "dashboard",
		Host:    "backend",
		Port:    8080,
	})
	if err != nil {
		t.Fatalf("addConfig: %v", err)
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
	for _, directive := range []string{
		"$upstream_http_x_hai_refreshed_access_cookie",
		"$upstream_http_x_hai_refreshed_refresh_cookie",
		"add_header Set-Cookie $hai_refreshed_access_cookie always;",
		"add_header Set-Cookie $hai_refreshed_refresh_cookie always;",
	} {
		if !strings.Contains(text, directive) {
			t.Errorf("config is missing refresh propagation directive %q", directive)
		}
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
