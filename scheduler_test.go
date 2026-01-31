package main

import (
	"strings"
	"testing"
	"time"
)

func TestNewScheduler_InvalidCron(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
		wantErr  string
	}{
		{
			name:     "empty expression",
			schedule: "",
			wantErr:  "empty spec string",
		},
		{
			name:     "too many fields",
			schedule: "* * * * * * * *",
			wantErr:  "expected",
		},
		{
			name:     "invalid characters",
			schedule: "abc def ghi jkl mno",
			wantErr:  "failed to parse",
		},
		{
			name:     "out of range minute",
			schedule: "61 * * * *",
			wantErr:  "end of range",
		},
		{
			name:     "out of range hour",
			schedule: "0 25 * * *",
			wantErr:  "end of range",
		},
	}

	cfg := &Config{
		SourcePath: "/src",
		RemoteHost: "u@h",
		RemotePath: "/dst",
		SSHKeyPath: "/key",
		LogDir:     t.TempDir(),
	}
	executor := NewBackupExecutor(cfg)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewScheduler(executor, tt.schedule)
			if err == nil {
				t.Fatalf("expected error for schedule %q", tt.schedule)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestNewScheduler_ValidCron(t *testing.T) {
	tests := []struct {
		name     string
		schedule string
	}{
		{"every minute", "* * * * *"},
		{"daily at 3am", "0 3 * * *"},
		{"weekly sunday", "0 0 * * 0"},
		{"every 6 hours", "0 */6 * * *"},
		{"specific days", "30 2 1,15 * *"},
	}

	cfg := &Config{
		SourcePath: "/src",
		RemoteHost: "u@h",
		RemotePath: "/dst",
		SSHKeyPath: "/key",
		LogDir:     t.TempDir(),
	}
	executor := NewBackupExecutor(cfg)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := NewScheduler(executor, tt.schedule)
			if err != nil {
				t.Fatalf("unexpected error for schedule %q: %v", tt.schedule, err)
			}
			sched.Start()
			defer sched.Stop()

			next := sched.NextRun()
			if next.Before(time.Now()) {
				t.Errorf("NextRun() = %v, expected a future time", next)
			}
		})
	}
}
