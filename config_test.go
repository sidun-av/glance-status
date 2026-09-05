package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

const validConfigYAML = `
grafana:
  url: http://grafana:3000
  token: tok
  datasource_uid: uid
metrics:
  cpu_query: cpu_expr
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
  disk_external_query: disk_e_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
`

func TestLoadConfig_Defaults(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Title != "Server Health" {
		t.Errorf("Title = %q, want default %q", cfg.Title, "Server Health")
	}
	if cfg.WarningThresholdPercent != 90 {
		t.Errorf("WarningThresholdPercent = %v, want default 90", cfg.WarningThresholdPercent)
	}
	if cfg.Grafana.URL != "http://grafana:3000" || cfg.Metrics.CPUQuery != "cpu_expr" {
		t.Errorf("unexpected parsed values: %+v", cfg)
	}
}

func TestLoadConfig_TitleEnvOverride(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML)
	t.Setenv("TITLE", "From Env")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Title != "From Env" {
		t.Errorf("Title = %q, want %q", cfg.Title, "From Env")
	}
}

func TestLoadConfig_ExpandsEnvPlaceholdersInConnectionFields(t *testing.T) {
	path := writeTempConfig(t, `
grafana:
  url: ${TEST_GRAFANA_URL}
  token: tok
  datasource_uid: uid
metrics:
  cpu_query: cpu_expr
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
  disk_external_query: disk_e_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
infra_checks:
  - name: npmplus
    check_url: ${TEST_NPMPLUS_URL}
`)
	t.Setenv("TEST_GRAFANA_URL", "http://real-grafana:3000")
	t.Setenv("TEST_NPMPLUS_URL", "http://real-npmplus:81")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Grafana.URL != "http://real-grafana:3000" {
		t.Errorf("Grafana.URL = %q, want expanded value", cfg.Grafana.URL)
	}
	if len(cfg.InfraChecks) != 1 || cfg.InfraChecks[0].CheckURL != "http://real-npmplus:81" {
		t.Errorf("InfraChecks = %+v, want expanded check_url", cfg.InfraChecks)
	}
}

func TestLoadConfig_DoesNotExpandPromQLQueries(t *testing.T) {
	path := writeTempConfig(t, `
grafana:
  url: http://grafana:3000
  token: tok
  datasource_uid: uid
metrics:
  cpu_query: '100 - $__rate_interval'
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
  disk_external_query: disk_e_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Metrics.CPUQuery != "100 - $__rate_interval" {
		t.Errorf("CPUQuery = %q, want untouched literal $ preserved", cfg.Metrics.CPUQuery)
	}
}

func TestLoadConfig_CustomWarningThreshold(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML+"\nwarning_threshold_percent: 80\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.WarningThresholdPercent != 80 {
		t.Errorf("WarningThresholdPercent = %v, want 80", cfg.WarningThresholdPercent)
	}
}

func TestLoadConfig_MissingGrafanaURLRejected(t *testing.T) {
	path := writeTempConfig(t, `
metrics:
  cpu_query: cpu_expr
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
  disk_external_query: disk_e_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error for missing grafana.url")
	}
}

func TestLoadConfig_MissingMetricQueryRejected(t *testing.T) {
	path := writeTempConfig(t, `
grafana:
  url: http://grafana:3000
  token: tok
  datasource_uid: uid
metrics:
  cpu_query: cpu_expr
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error for missing metrics.disk_external_query")
	}
}

func TestLoadConfig_InfraCheckMissingURLRejected(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML+"\ninfra_checks:\n  - name: npmplus\n")
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error for infra_checks entry missing check_url")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	if _, err := LoadConfig("/nonexistent/config.yml"); err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestLoadConfig_ExampleFileIsValid(t *testing.T) {
	// config.example.yml uses ${VAR} placeholders for connection fields —
	// set them so LoadConfig's required-field checks pass, proving the
	// file's structure (not its placeholder values) is valid.
	for _, v := range []string{"GRAFANA_URL", "GRAFANA_TOKEN", "SPEEDTEST_URL", "SPEEDTEST_TOKEN", "SERVICES_STATUS_URL", "INFRA_CHECK_NPMPLUS_URL", "INFRA_CHECK_HOMEASSISTANT_URL"} {
		t.Setenv(v, "http://placeholder")
	}
	cfg, err := LoadConfig("config.example.yml")
	if err != nil {
		t.Fatalf("LoadConfig(config.example.yml): %v", err)
	}
	if len(cfg.InfraChecks) != 2 {
		t.Errorf("len(InfraChecks) = %d, want 2", len(cfg.InfraChecks))
	}
}
