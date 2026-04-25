package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig   `yaml:"server"`
	Sources []SourceConfig `yaml:"sources"`
}

type ServerConfig struct {
	Port    int    `yaml:"port"`
	DataDir string `yaml:"data_dir"`
}

type SourceConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	// github_wiki, github_repo
	Repo   string `yaml:"repo,omitempty"`
	Branch string `yaml:"branch,omitempty"`
	// web
	URL         string `yaml:"url,omitempty"`
	IncludePath string `yaml:"include_path,omitempty"`
	Auth        string `yaml:"auth,omitempty"`
	// confluence
	BaseURL  string `yaml:"base_url,omitempty"`
	SpaceKey string `yaml:"space_key,omitempty"`
	// scheduling
	CrawlSchedule string `yaml:"crawl_schedule,omitempty"`
	// Token is populated at runtime from the token store and never read from YAML.
	Token string `yaml:"-"`
}

// defaults returns a Config populated with the built-in defaults used when no
// config file is present or fields are omitted.
func defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Port:    8080,
			DataDir: "/app/data",
		},
	}
}

// applyDefaults applies built-in defaults to cfg for any zero-valued fields.
func applyDefaults(cfg *Config) {
	d := defaults()
	if cfg.Server.Port == 0 {
		cfg.Server.Port = d.Server.Port
	}
	if cfg.Server.DataDir == "" {
		cfg.Server.DataDir = d.Server.DataDir
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

// LoadOrDefault is like Load but returns built-in defaults when the file does
// not exist. Other errors (parse failures, permission denied, etc.) still
// propagate so misconfiguration is loud, not silent.
func LoadOrDefault(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Info("no config file found, using built-in defaults", "path", path)
			return defaults(), nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}
