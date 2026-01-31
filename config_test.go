package main

import (
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

func TestLoadConfig_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "missing source_path",
			yaml:    "remote_host: h\nremote_path: p\nssh_key_path: k\nschedule: '0 * * * *'",
			wantErr: "source_path is required",
		},
		{
			name:    "missing remote_host",
			yaml:    "source_path: s\nremote_path: p\nssh_key_path: k\nschedule: '0 * * * *'",
			wantErr: "remote_host is required",
		},
		{
			name:    "missing remote_path",
			yaml:    "source_path: s\nremote_host: h\nssh_key_path: k\nschedule: '0 * * * *'",
			wantErr: "remote_path is required",
		},
		{
			name:    "missing ssh_key_path",
			yaml:    "source_path: s\nremote_host: h\nremote_path: p\nschedule: '0 * * * *'",
			wantErr: "ssh_key_path is required",
		},
		{
			name:    "missing schedule",
			yaml:    "source_path: s\nremote_host: h\nremote_path: p\nssh_key_path: k",
			wantErr: "schedule is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeTestConfig(t, dir, tt.yaml)
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
