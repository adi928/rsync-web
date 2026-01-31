package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helper: fake rsync via TestHelperProcess pattern
// See https://npf.io/2015/06/testing-exec-command/
// ---------------------------------------------------------------------------

// fakeRsyncCmd returns a CmdFactory that re-invokes the test binary with
// GO_TEST_PROCESS=1, passing the desired exit code and optional stdout text
// as environment variables.
func fakeRsyncCmd(exitCode int, output string) CmdFactory {
	return func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_TEST_PROCESS=1",
			fmt.Sprintf("GO_TEST_EXIT_CODE=%d", exitCode),
			fmt.Sprintf("GO_TEST_OUTPUT=%s", output),
		)
		return cmd
	}
}

// TestHelperProcess is invoked by the fake command factory. It is not a real test.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	output := os.Getenv("GO_TEST_OUTPUT")
	if output != "" {
		fmt.Fprint(os.Stdout, output)
	}
	exitCode := 0
	fmt.Sscanf(os.Getenv("GO_TEST_EXIT_CODE"), "%d", &exitCode)
	os.Exit(exitCode)
}

// testConfig returns a Config pointing at temporary directories.
func testConfig(t *testing.T) *Config {
	t.Helper()
	logDir := filepath.Join(t.TempDir(), "logs")
	return &Config{
		SourcePath:  "/mnt/plex-media",
		RemoteHost:  "user@backup-host",
		RemotePath:  "/backups/plex",
		SSHKeyPath:  "~/.ssh/test_key",
		Schedule:    "0 3 * * *",
		LogDir:      logDir,
		MaxLogFiles: 5,
	}
}

// waitForStatus polls the executor until it reaches the desired status or times out.
func waitForStatus(ex *BackupExecutor, want BackupStatus, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ex.Status() == want {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for status %q, current: %q", want, ex.Status())
}

// ---------------------------------------------------------------------------
// buildRsyncArgs tests
// ---------------------------------------------------------------------------

func TestBuildRsyncArgs_ContainsPartialFlag(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()

	found := false
	for _, arg := range args {
		if arg == "--partial" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --partial in rsync args, got: %v", args)
	}
}

func TestBuildRsyncArgs_ContainsDeleteFlag(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()

	found := false
	for _, arg := range args {
		if arg == "--delete" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --delete in rsync args, got: %v", args)
	}
}

func TestBuildRsyncArgs_SSHKeyInCommand(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, cfg.SSHKeyPath) {
		t.Errorf("expected SSH key path %q in args: %s", cfg.SSHKeyPath, joined)
	}
}

func TestBuildRsyncArgs_BandwidthLimit(t *testing.T) {
	cfg := testConfig(t)
	cfg.BandwidthLimit = 5000
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()

	found := false
	for _, arg := range args {
		if arg == "--bwlimit=5000" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --bwlimit=5000 in args, got: %v", args)
	}
}

func TestBuildRsyncArgs_NoBandwidthLimitWhenZero(t *testing.T) {
	cfg := testConfig(t)
	cfg.BandwidthLimit = 0
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()

	for _, arg := range args {
		if strings.HasPrefix(arg, "--bwlimit") {
			t.Errorf("unexpected --bwlimit in args when limit is 0: %v", args)
		}
	}
}

func TestBuildRsyncArgs_SourceTrailingSlash(t *testing.T) {
	cfg := testConfig(t)
	cfg.SourcePath = "/mnt/plex-media"
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()
	source := args[len(args)-2]
	if !strings.HasSuffix(source, "/") {
		t.Errorf("source should end with /, got: %q", source)
	}

	// Also test when source already has trailing slash
	cfg.SourcePath = "/mnt/plex-media/"
	args = ex.buildRsyncArgs()
	source = args[len(args)-2]
	if strings.HasSuffix(source, "//") {
		t.Errorf("source should not have double slash, got: %q", source)
	}
}

func TestBuildRsyncArgs_DestinationFormat(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()
	dest := args[len(args)-1]
	expected := "user@backup-host:/backups/plex/"
	if dest != expected {
		t.Errorf("destination = %q, want %q", dest, expected)
	}
}

// ---------------------------------------------------------------------------
// File source support
// ---------------------------------------------------------------------------

func TestBuildRsyncArgs_FileSource(t *testing.T) {
	cfg := testConfig(t)
	cfg.SourcePath = "/mnt/plex-media/archive.tar.gz"
	cfg.SourceIsFile = true
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()
	source := args[len(args)-2]

	// File source should NOT have a trailing slash
	if strings.HasSuffix(source, "/") {
		t.Errorf("file source should not end with /, got: %q", source)
	}
	if source != "/mnt/plex-media/archive.tar.gz" {
		t.Errorf("source = %q, want /mnt/plex-media/archive.tar.gz", source)
	}
}

func TestBuildRsyncArgs_DirectorySource(t *testing.T) {
	cfg := testConfig(t)
	cfg.SourcePath = "/mnt/plex-media"
	cfg.SourceIsFile = false
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()
	source := args[len(args)-2]

	// Directory source MUST have a trailing slash
	if !strings.HasSuffix(source, "/") {
		t.Errorf("directory source should end with /, got: %q", source)
	}
	if source != "/mnt/plex-media/" {
		t.Errorf("source = %q, want /mnt/plex-media/", source)
	}
}

func TestBuildRsyncArgs_FileSourceDefaultFalse(t *testing.T) {
	cfg := testConfig(t)
	cfg.SourcePath = "/mnt/plex-media"
	// SourceIsFile defaults to false (zero value)
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()
	source := args[len(args)-2]

	if !strings.HasSuffix(source, "/") {
		t.Errorf("default (directory) source should end with /, got: %q", source)
	}
}

// ---------------------------------------------------------------------------
// Remote path check
// ---------------------------------------------------------------------------

func TestCheckRemotePath_NonEmpty(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)
	// Simulate SSH ls returning file listing
	ex.cmdFactory = func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_TEST_PROCESS=1",
			"GO_TEST_EXIT_CODE=0",
			"GO_TEST_OUTPUT=movies\ntv-shows\nmusic",
		)
		return cmd
	}

	nonEmpty, files, err := ex.CheckRemotePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nonEmpty {
		t.Error("expected non-empty remote path")
	}
	if len(files) != 3 {
		t.Errorf("expected 3 files, got %d: %v", len(files), files)
	}
}

func TestCheckRemotePath_Empty(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)
	// Simulate SSH ls returning empty output
	ex.cmdFactory = func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_TEST_PROCESS=1",
			"GO_TEST_EXIT_CODE=0",
			"GO_TEST_OUTPUT=",
		)
		return cmd
	}

	nonEmpty, files, err := ex.CheckRemotePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nonEmpty {
		t.Error("expected empty remote path")
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestCheckRemotePath_SSHFailure(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)
	// Simulate SSH connection failure
	ex.cmdFactory = fakeRsyncCmd(255, "")

	_, _, err := ex.CheckRemotePath()
	if err == nil {
		t.Error("expected error for SSH failure")
	}
}

// ---------------------------------------------------------------------------
// Unreachable backup target
// ---------------------------------------------------------------------------

func TestBackup_UnreachableTarget(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)
	// Simulate rsync exit code 255: SSH connection failed (unreachable host)
	ex.cmdFactory = fakeRsyncCmd(255, "ssh: connect to host backup-host port 22: Connection refused\nrsync error: unexplained error (code 255)")

	err := ex.Run()
	if err != nil {
		t.Fatalf("Run() should not return error (it starts async): %v", err)
	}

	if err := waitForStatus(ex, StatusFailed, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	last := ex.LastRun()
	if last == nil {
		t.Fatal("expected a history entry after failed run")
	}
	if last.ExitCode != 255 {
		t.Errorf("exit code = %d, want 255", last.ExitCode)
	}
	if last.Status != StatusFailed {
		t.Errorf("status = %q, want %q", last.Status, StatusFailed)
	}

	// Verify the failure is logged
	logContent, err := ex.ReadLog(last.LogFile)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if !strings.Contains(logContent, "exit code: 255") {
		t.Errorf("log should mention exit code 255, got:\n%s", logContent)
	}
}

// ---------------------------------------------------------------------------
// Invalid SSH key (rsync exits 255 when SSH auth fails)
// ---------------------------------------------------------------------------

func TestBackup_InvalidSSHKey(t *testing.T) {
	cfg := testConfig(t)
	cfg.SSHKeyPath = "/nonexistent/bad_key"
	ex := NewBackupExecutor(cfg)
	// SSH will fail with permission denied / no such file when key is invalid
	ex.cmdFactory = fakeRsyncCmd(255, "Warning: Identity file /nonexistent/bad_key not accessible: No such file or directory.\nPermission denied (publickey).\nrsync error: unexplained error (code 255)")

	err := ex.Run()
	if err != nil {
		t.Fatalf("Run() should not return error: %v", err)
	}

	if err := waitForStatus(ex, StatusFailed, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	last := ex.LastRun()
	if last.ExitCode != 255 {
		t.Errorf("exit code = %d, want 255", last.ExitCode)
	}
	if last.Status != StatusFailed {
		t.Errorf("status = %q, want %q", last.Status, StatusFailed)
	}
}

// ---------------------------------------------------------------------------
// Partial failure (rsync exit code 23: partial transfer due to error)
// ---------------------------------------------------------------------------

func TestBackup_PartialFailure(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)
	// Exit code 23: some files could not be transferred (permission denied, vanished, etc.)
	ex.cmdFactory = fakeRsyncCmd(23, `sending incremental file list
media/movies/file1.mkv
rsync: send_files failed to open "/mnt/plex-media/media/movies/restricted.mkv": Permission denied (13)
media/movies/file2.mkv

Number of files: 100
Number of files transferred: 98
Total file size: 500,000,000 bytes
Total transferred file size: 490,000,000 bytes

rsync error: some files/attrs were not transferred (code 23)`)

	err := ex.Run()
	if err != nil {
		t.Fatalf("Run() should not return error: %v", err)
	}

	if err := waitForStatus(ex, StatusWarning, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	last := ex.LastRun()
	if last == nil {
		t.Fatal("expected a history entry")
	}
	if last.ExitCode != 23 {
		t.Errorf("exit code = %d, want 23 (partial transfer)", last.ExitCode)
	}
	if last.Status != StatusWarning {
		t.Errorf("status = %q, want %q", last.Status, StatusWarning)
	}

	// The log should contain the rsync partial transfer output
	logContent, err := ex.ReadLog(last.LogFile)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if !strings.Contains(logContent, "Permission denied") {
		t.Errorf("log should contain permission denied message, got:\n%s", logContent)
	}
	if !strings.Contains(logContent, "code 23") {
		t.Errorf("log should mention code 23, got:\n%s", logContent)
	}
}

// ---------------------------------------------------------------------------
// Vanished source files (rsync exit code 24: partial transfer, warning)
// ---------------------------------------------------------------------------

func TestBackup_VanishedSourceFiles(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)
	// Exit code 24: some source files vanished during the sync (common in media libraries)
	ex.cmdFactory = fakeRsyncCmd(24, `sending incremental file list
media/movies/movie.mkv
file has vanished: "/mnt/plex-media/media/movies/temp_encode.mkv"

rsync warning: some files vanished before they could be transferred (code 24)`)

	err := ex.Run()
	if err != nil {
		t.Fatalf("Run() should not return error: %v", err)
	}

	if err := waitForStatus(ex, StatusWarning, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	last := ex.LastRun()
	if last == nil {
		t.Fatal("expected a history entry")
	}
	if last.ExitCode != 24 {
		t.Errorf("exit code = %d, want 24", last.ExitCode)
	}
	if last.Status != StatusWarning {
		t.Errorf("status = %q, want %q (vanished files are a warning, not a failure)", last.Status, StatusWarning)
	}
	if !strings.Contains(last.Summary, "vanished") {
		t.Errorf("summary = %q, want it to mention vanished files", last.Summary)
	}
}

// ---------------------------------------------------------------------------
// rsyncExitSummary
// ---------------------------------------------------------------------------

func TestRsyncExitSummary(t *testing.T) {
	tests := []struct {
		code    int
		wantSub string
	}{
		{0, ""},   // code 0 never hits rsyncExitSummary in practice
		{1, "syntax"},
		{23, "partial transfer"},
		{24, "vanished"},
		{255, "SSH connection failed"},
		{99, "exit code 99"},
	}

	for _, tt := range tests {
		if tt.code == 0 {
			continue
		}
		t.Run(fmt.Sprintf("exit_%d", tt.code), func(t *testing.T) {
			summary := rsyncExitSummary(tt.code)
			if !strings.Contains(summary, tt.wantSub) {
				t.Errorf("rsyncExitSummary(%d) = %q, want it to contain %q", tt.code, summary, tt.wantSub)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isPartialTransfer
// ---------------------------------------------------------------------------

func TestIsPartialTransfer(t *testing.T) {
	if !isPartialTransfer(23) {
		t.Error("exit code 23 should be a partial transfer")
	}
	if !isPartialTransfer(24) {
		t.Error("exit code 24 should be a partial transfer")
	}
	if isPartialTransfer(0) {
		t.Error("exit code 0 should not be a partial transfer")
	}
	if isPartialTransfer(255) {
		t.Error("exit code 255 should not be a partial transfer")
	}
	if isPartialTransfer(1) {
		t.Error("exit code 1 should not be a partial transfer")
	}
}

// ---------------------------------------------------------------------------
// Successful backup
// ---------------------------------------------------------------------------

func TestBackup_Success(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)
	ex.cmdFactory = fakeRsyncCmd(0, `sending incremental file list

Number of files: 1,234
Number of files transferred: 12
Total file size: 800,000,000,000 bytes
Total transferred file size: 5,000,000 bytes

sent 5,100,000 bytes  received 300 bytes  1,020,060.00 bytes/sec
total size is 800,000,000,000  speedup is 156,862.75`)

	err := ex.Run()
	if err != nil {
		t.Fatalf("Run() should not return error: %v", err)
	}

	if err := waitForStatus(ex, StatusSuccess, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	last := ex.LastRun()
	if last == nil {
		t.Fatal("expected a history entry")
	}
	if last.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", last.ExitCode)
	}
	if last.Status != StatusSuccess {
		t.Errorf("status = %q, want %q", last.Status, StatusSuccess)
	}
	if last.Summary != "completed successfully" {
		t.Errorf("summary = %q, want 'completed successfully'", last.Summary)
	}
}

// ---------------------------------------------------------------------------
// Concurrent backup prevention
// ---------------------------------------------------------------------------

func TestBackup_ConcurrentPrevention(t *testing.T) {
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)
	// Use a slow fake command (sleep) so the first backup is still running
	ex.cmdFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sleep", "5")
	}

	// Start first backup
	err := ex.Run()
	if err != nil {
		t.Fatalf("first Run() should succeed: %v", err)
	}

	// Wait until it's running
	if err := waitForStatus(ex, StatusRunning, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	// Try second backup — should be rejected
	err = ex.Run()
	if err == nil {
		t.Fatal("second Run() should return error when backup already in progress")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error = %q, want it to mention 'already in progress'", err)
	}
}

// ---------------------------------------------------------------------------
// Resume: verify --partial flag enables rsync resume behavior
// ---------------------------------------------------------------------------

func TestBuildRsyncArgs_PartialFlagForResume(t *testing.T) {
	// The --partial flag tells rsync to keep partially transferred files,
	// so re-running rsync after an interruption resumes from where it left off
	// instead of re-transferring from scratch.
	cfg := testConfig(t)
	ex := NewBackupExecutor(cfg)

	args := ex.buildRsyncArgs()
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--partial") {
		t.Fatal("rsync args must contain --partial for resume support")
	}

	// --partial should appear alongside --delete (mirror mode)
	if !strings.Contains(joined, "--delete") {
		t.Fatal("rsync args must contain --delete for mirror mode")
	}

	// --stats should be present for transfer reporting
	if !strings.Contains(joined, "--stats") {
		t.Fatal("rsync args must contain --stats for transfer statistics")
	}
}

// ---------------------------------------------------------------------------
// History management
// ---------------------------------------------------------------------------

func TestHistory_PersistsAcrossRestarts(t *testing.T) {
	cfg := testConfig(t)

	// First executor: run a backup
	ex1 := NewBackupExecutor(cfg)
	ex1.cmdFactory = fakeRsyncCmd(0, "ok")

	if err := ex1.Run(); err != nil {
		t.Fatal(err)
	}
	if err := waitForStatus(ex1, StatusSuccess, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	if len(ex1.History()) != 1 {
		t.Fatalf("history length = %d, want 1", len(ex1.History()))
	}

	// Second executor: should load history from disk
	ex2 := NewBackupExecutor(cfg)
	if len(ex2.History()) != 1 {
		t.Fatalf("reloaded history length = %d, want 1", len(ex2.History()))
	}
	if ex2.Status() != StatusSuccess {
		t.Errorf("reloaded status = %q, want %q", ex2.Status(), StatusSuccess)
	}
}

func TestHistory_CappedAt100(t *testing.T) {
	cfg := testConfig(t)

	// Write 105 fake history entries directly
	var entries []BackupRun
	for i := 0; i < 105; i++ {
		entries = append(entries, BackupRun{
			ID:        fmt.Sprintf("run-%03d", i),
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Status:    StatusSuccess,
			LogFile:   fmt.Sprintf("backup-run-%03d.log", i),
		})
	}

	os.MkdirAll(cfg.LogDir, 0755)
	data, _ := json.MarshalIndent(entries, "", "  ")
	os.WriteFile(filepath.Join(cfg.LogDir, "history.json"), data, 0644)

	ex := NewBackupExecutor(cfg)
	ex.cmdFactory = fakeRsyncCmd(0, "ok")

	// Run one more backup — history should be capped
	if err := ex.Run(); err != nil {
		t.Fatal(err)
	}
	if err := waitForStatus(ex, StatusSuccess, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	history := ex.History()
	if len(history) > 100 {
		t.Errorf("history length = %d, want <= 100", len(history))
	}
}

// ---------------------------------------------------------------------------
// Log pruning
// ---------------------------------------------------------------------------

func TestLogPruning(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxLogFiles = 3
	os.MkdirAll(cfg.LogDir, 0755)

	ex := NewBackupExecutor(cfg)
	ex.cmdFactory = fakeRsyncCmd(0, "ok")

	// Run 5 backups
	for i := 0; i < 5; i++ {
		if err := ex.Run(); err != nil {
			t.Fatal(err)
		}
		if err := waitForStatus(ex, StatusSuccess, 10*time.Second); err != nil {
			t.Fatal(err)
		}
		// Small delay so log filenames differ (they use second-precision timestamps)
		time.Sleep(1100 * time.Millisecond)
	}

	// Count .log files
	entries, _ := os.ReadDir(cfg.LogDir)
	logCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") {
			logCount++
		}
	}

	if logCount > cfg.MaxLogFiles {
		t.Errorf("log file count = %d, want <= %d", logCount, cfg.MaxLogFiles)
	}
}

// ---------------------------------------------------------------------------
// Log reading — path traversal prevention
// ---------------------------------------------------------------------------

func TestReadLog_PathTraversalPrevention(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(cfg.LogDir, 0755)
	ex := NewBackupExecutor(cfg)

	badPaths := []string{
		"../../../etc/passwd",
		"foo/../../etc/shadow",
		`backup\..\..\..\windows\system32`,
	}

	for _, p := range badPaths {
		_, err := ex.ReadLog(p)
		if err == nil {
			t.Errorf("ReadLog(%q) should return error for path traversal", p)
		}
	}
}

func TestReadLog_ValidFile(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(cfg.LogDir, 0755)
	os.WriteFile(filepath.Join(cfg.LogDir, "backup-test.log"), []byte("test log content"), 0644)
	ex := NewBackupExecutor(cfg)

	content, err := ex.ReadLog("backup-test.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "test log content" {
		t.Errorf("content = %q, want 'test log content'", content)
	}
}
