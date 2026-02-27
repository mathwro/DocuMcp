package config

import (
	"fmt"
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
	// github_wiki
	Repo string `yaml:"repo,omitempty"`
	// web
	URL  string `yaml:"url,omitempty"`
	Auth string `yaml:"auth,omitempty"`
	// confluence
	BaseURL  string `yaml:"base_url,omitempty"`
	SpaceKey string `yaml:"space_key,omitempty"`
	// scheduling
	CrawlSchedule string `yaml:"crawl_schedule,omitempty"`
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
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.DataDir == "" {
		cfg.Server.DataDir = "/app/data"
	}
	return &cfg, nil
}
