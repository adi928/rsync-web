package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
	if c.Schedule == "" {
		return fmt.Errorf("schedule is required")
	}
	return nil
}

// TransferConfigured returns true if all transfer-related settings are set.
func (c *Config) TransferConfigured() bool {
	return c.SourcePath != "" && c.RemoteHost != "" && c.RemotePath != "" && c.SSHKeyPath != ""
}

// SettingsFilePath returns the path to the persisted transfer settings file.
func (c *Config) SettingsFilePath() string {
	return filepath.Join(c.LogDir, "settings.json")
}

// TransferSettings holds the user-configurable transfer fields.
type TransferSettings struct {
	SourcePath   string `json:"source_path"`
	SourceIsFile bool   `json:"source_is_file"`
	RemoteHost   string `json:"remote_host"`
	RemotePath   string `json:"remote_path"`
	SSHKeyPath   string `json:"ssh_key_path"`
}

// ApplyTransferSettings updates the config with values from TransferSettings.
func (c *Config) ApplyTransferSettings(s TransferSettings) {
	c.SourcePath = s.SourcePath
	c.SourceIsFile = s.SourceIsFile
	c.RemoteHost = s.RemoteHost
	c.RemotePath = s.RemotePath
	c.SSHKeyPath = s.SSHKeyPath
}

// GetTransferSettings extracts the current transfer settings from the config.
func (c *Config) GetTransferSettings() TransferSettings {
	return TransferSettings{
		SourcePath:   c.SourcePath,
		SourceIsFile: c.SourceIsFile,
		RemoteHost:   c.RemoteHost,
		RemotePath:   c.RemotePath,
		SSHKeyPath:   c.SSHKeyPath,
	}
}

// LoadTransferSettings reads transfer settings from the settings file and applies them.
func (c *Config) LoadTransferSettings() error {
	data, err := os.ReadFile(c.SettingsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no saved settings yet
		}
		return fmt.Errorf("reading settings file: %w", err)
	}
	var s TransferSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("parsing settings file: %w", err)
	}
	c.ApplyTransferSettings(s)
	return nil
}

// SaveTransferSettings writes the current transfer settings to the settings file.
func (c *Config) SaveTransferSettings() error {
	if err := os.MkdirAll(filepath.Dir(c.SettingsFilePath()), 0755); err != nil {
		return fmt.Errorf("creating settings directory: %w", err)
	}
	data, err := json.MarshalIndent(c.GetTransferSettings(), "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling settings: %w", err)
	}
	if err := os.WriteFile(c.SettingsFilePath(), data, 0644); err != nil {
		return fmt.Errorf("writing settings file: %w", err)
	}
	return nil
}
