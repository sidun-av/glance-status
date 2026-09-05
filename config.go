package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type GrafanaConfig struct {
	URL           string `yaml:"url"`
	Token         string `yaml:"token"`
	DatasourceUID string `yaml:"datasource_uid"`
}

type MetricsConfig struct {
	CPUQuery          string `yaml:"cpu_query"`
	RAMQuery          string `yaml:"ram_query"`
	DiskInternalQuery string `yaml:"disk_internal_query"`
	DiskExternalQuery string `yaml:"disk_external_query"`
}

type SpeedtestConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type InfraCheckConfig struct {
	Name     string `yaml:"name"`
	CheckURL string `yaml:"check_url"`
}

type Config struct {
	Title                   string             `yaml:"title"`
	Grafana                 GrafanaConfig      `yaml:"grafana"`
	Metrics                 MetricsConfig      `yaml:"metrics"`
	Speedtest               SpeedtestConfig    `yaml:"speedtest"`
	ServicesStatusURL       string             `yaml:"services_status_url"`
	InfraChecks             []InfraCheckConfig `yaml:"infra_checks"`
	WarningThresholdPercent float64            `yaml:"warning_threshold_percent"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Only connection-y fields get environment variable expansion (same
	// convention as glance-grafana-sparkline's config.go). PromQL query
	// text is deliberately left untouched so a literal "$" in a query
	// (e.g. a Grafana macro like $__rate_interval) is never silently
	// replaced with an empty string.
	cfg.Grafana.URL = os.Expand(cfg.Grafana.URL, os.Getenv)
	cfg.Grafana.Token = os.Expand(cfg.Grafana.Token, os.Getenv)
	cfg.Grafana.DatasourceUID = os.Expand(cfg.Grafana.DatasourceUID, os.Getenv)
	cfg.Speedtest.URL = os.Expand(cfg.Speedtest.URL, os.Getenv)
	cfg.Speedtest.Token = os.Expand(cfg.Speedtest.Token, os.Getenv)
	cfg.ServicesStatusURL = os.Expand(cfg.ServicesStatusURL, os.Getenv)
	for i := range cfg.InfraChecks {
		cfg.InfraChecks[i].CheckURL = os.Expand(cfg.InfraChecks[i].CheckURL, os.Getenv)
	}

	if v, ok := lookupNonEmptyEnv("TITLE"); ok {
		cfg.Title = v
	}
	if cfg.Title == "" {
		cfg.Title = "Server Health"
	}
	if cfg.WarningThresholdPercent == 0 {
		cfg.WarningThresholdPercent = 90
	}

	if cfg.Grafana.URL == "" {
		return nil, fmt.Errorf("grafana.url is required")
	}
	if cfg.Grafana.Token == "" {
		return nil, fmt.Errorf("grafana.token is required")
	}
	if cfg.Grafana.DatasourceUID == "" {
		return nil, fmt.Errorf("grafana.datasource_uid is required")
	}
	if cfg.Metrics.CPUQuery == "" || cfg.Metrics.RAMQuery == "" || cfg.Metrics.DiskInternalQuery == "" || cfg.Metrics.DiskExternalQuery == "" {
		return nil, fmt.Errorf("metrics: all four queries (cpu_query, ram_query, disk_internal_query, disk_external_query) are required")
	}
	if cfg.Speedtest.URL == "" {
		return nil, fmt.Errorf("speedtest.url is required")
	}
	if cfg.Speedtest.Token == "" {
		return nil, fmt.Errorf("speedtest.token is required")
	}
	if cfg.ServicesStatusURL == "" {
		return nil, fmt.Errorf("services_status_url is required")
	}
	for i, c := range cfg.InfraChecks {
		if c.Name == "" || c.CheckURL == "" {
			return nil, fmt.Errorf("infra_checks[%d]: name and check_url are both required", i)
		}
	}

	return &cfg, nil
}

// lookupNonEmptyEnv returns (value, true) only when the environment variable
// is actually set to a non-empty string — matching the sibling repos'
// convention, so an unfilled-in Komodo stack variable (which arrives as an
// empty string, not unset) doesn't override a real YAML value with "".
func lookupNonEmptyEnv(name string) (string, bool) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
