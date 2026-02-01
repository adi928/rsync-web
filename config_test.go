package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

func TestLoadConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `
source_path: /mnt/plex-media
remote_host: user@backup-server
remote_path: /backups/plex
ssh_key_path: ~/.ssh/id_rsa
schedule: "0 3 * * *"
bandwidth_limit: 5000
listen_addr: ":9090"
log_dir: ./test-logs
max_log_files: 10
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.SourcePath != "/mnt/plex-media" {
		t.Errorf("source_path = %q, want /mnt/plex-media", cfg.SourcePath)
	}
	if cfg.RemoteHost != "user@backup-server" {
		t.Errorf("remote_host = %q, want user@backup-server", cfg.RemoteHost)
	}
	if cfg.BandwidthLimit != 5000 {
		t.Errorf("bandwidth_limit = %d, want 5000", cfg.BandwidthLimit)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("listen_addr = %q, want :9090", cfg.ListenAddr)
	}
	if cfg.MaxLogFiles != 10 {
		t.Errorf("max_log_files = %d, want 10", cfg.MaxLogFiles)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `
source_path: /data
remote_host: user@host
remote_path: /backup
ssh_key_path: ~/.ssh/key
schedule: "0 * * * *"
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.ListenAddr != ":8090" {
		t.Errorf("default listen_addr = %q, want :8090", cfg.ListenAddr)
	}
	if cfg.LogDir != "./logs" {
		t.Errorf("default log_dir = %q, want ./logs", cfg.LogDir)
	}
	if cfg.MaxLogFiles != 30 {
		t.Errorf("default max_log_files = %d, want 30", cfg.MaxLogFiles)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "reading config file") {
		t.Errorf("error = %q, want it to mention 'reading config file'", err)
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `{{{invalid yaml`)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if !strings.Contains(err.Error(), "parsing config file") {
		t.Errorf("error = %q, want it to mention 'parsing config file'", err)
	}
}

func TestLoadConfig_MissingSchedule(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "source_path: s\nremote_host: h\nremote_path: p\nssh_key_path: k")
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing schedule")
	}
	if !strings.Contains(err.Error(), "schedule is required") {
		t.Errorf("error = %q, want it to mention 'schedule is required'", err)
	}
}

func TestLoadConfig_TransferFieldsOptional(t *testing.T) {
	// Config should load successfully without transfer fields — they are set via the web UI
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `schedule: "0 3 * * *"`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.TransferConfigured() {
		t.Error("TransferConfigured() should be false when transfer fields are empty")
	}
}

func TestTransferConfigured(t *testing.T) {
	cfg := &Config{
		SourcePath: "/src",
		RemoteHost: "u@h",
		RemotePath: "/dst",
		SSHKeyPath: "/key",
		Schedule:   "0 3 * * *",
	}
	if !cfg.TransferConfigured() {
		t.Error("TransferConfigured() should be true when all fields are set")
	}

	cfg.SSHKeyPath = ""
	if cfg.TransferConfigured() {
		t.Error("TransferConfigured() should be false when ssh_key_path is empty")
	}
}

func TestTransferSettings_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Schedule: "0 3 * * *",
		LogDir:   dir,
	}

	// Apply and save
	cfg.ApplyTransferSettings(TransferSettings{
		SourcePath:   "/mnt/data",
		SourceIsFile: true,
		RemoteHost:   "user@host",
		RemotePath:   "/backups",
		SSHKeyPath:   "~/.ssh/key",
	})
	if err := cfg.SaveTransferSettings(); err != nil {
		t.Fatalf("SaveTransferSettings() error: %v", err)
	}

	// Verify file was written
	data, err := os.ReadFile(cfg.SettingsFilePath())
	if err != nil {
		t.Fatalf("settings file not found: %v", err)
	}
	var saved TransferSettings
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("invalid JSON in settings file: %v", err)
	}
	if saved.SourcePath != "/mnt/data" {
		t.Errorf("saved source_path = %q, want /mnt/data", saved.SourcePath)
	}
	if !saved.SourceIsFile {
		t.Error("saved source_is_file should be true")
	}

	// Load into a fresh config
	cfg2 := &Config{
		Schedule: "0 3 * * *",
		LogDir:   dir,
	}
	if err := cfg2.LoadTransferSettings(); err != nil {
		t.Fatalf("LoadTransferSettings() error: %v", err)
	}
	if cfg2.SourcePath != "/mnt/data" {
		t.Errorf("loaded source_path = %q, want /mnt/data", cfg2.SourcePath)
	}
	if cfg2.RemoteHost != "user@host" {
		t.Errorf("loaded remote_host = %q, want user@host", cfg2.RemoteHost)
	}
	if !cfg2.TransferConfigured() {
		t.Error("TransferConfigured() should be true after loading settings")
	}
}

func TestLoadTransferSettings_NoFile(t *testing.T) {
	cfg := &Config{
		Schedule: "0 3 * * *",
		LogDir:   t.TempDir(),
	}
	// Should not error when settings file doesn't exist
	if err := cfg.LoadTransferSettings(); err != nil {
		t.Fatalf("LoadTransferSettings() should not error for missing file: %v", err)
	}
}
