package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	SourcePath     string `yaml:"source_path"`
	SourceIsFile   bool   `yaml:"source_is_file"`
	RemoteHost     string `yaml:"remote_host"`
	RemotePath     string `yaml:"remote_path"`
	SSHKeyPath     string `yaml:"ssh_key_path"`
	Schedule       string `yaml:"schedule"`
	BandwidthLimit int    `yaml:"bandwidth_limit"`
	ListenAddr     string `yaml:"listen_addr"`
	LogDir         string `yaml:"log_dir"`
	MaxLogFiles    int    `yaml:"max_log_files"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := &Config{
		ListenAddr:  ":8090",
		LogDir:      "./logs",
		MaxLogFiles: 30,
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.SourcePath == "" {
		return fmt.Errorf("source_path is required")
	}
	if c.RemoteHost == "" {
		return fmt.Errorf("remote_host is required")
	}
	if c.RemotePath == "" {
		return fmt.Errorf("remote_path is required")
	}
	if c.SSHKeyPath == "" {
		return fmt.Errorf("ssh_key_path is required")
	}
	if c.Schedule == "" {
		return fmt.Errorf("schedule is required")
	}
	return nil
}
