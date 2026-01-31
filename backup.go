package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type BackupStatus string

const (
	StatusIdle    BackupStatus = "idle"
	StatusRunning BackupStatus = "running"
	StatusSuccess BackupStatus = "success"
	StatusWarning BackupStatus = "warning"
	StatusFailed  BackupStatus = "failed"
)

type BackupRun struct {
	ID        string       `json:"id"`
	StartTime time.Time    `json:"start_time"`
	EndTime   time.Time    `json:"end_time,omitempty"`
	Duration  string       `json:"duration,omitempty"`
	Status    BackupStatus `json:"status"`
	ExitCode  int          `json:"exit_code"`
	LogFile   string       `json:"log_file"`
	Summary   string       `json:"summary,omitempty"`
}

// CmdFactory creates an *exec.Cmd for the given program and arguments.
// Defaults to exec.Command; tests can override this to inject fakes.
type CmdFactory func(name string, args ...string) *exec.Cmd

type BackupExecutor struct {
	cfg        *Config
	mu         sync.Mutex
	status     BackupStatus
	current    *BackupRun
	history    []BackupRun
	cmdFactory CmdFactory
}

func NewBackupExecutor(cfg *Config) *BackupExecutor {
	ex := &BackupExecutor{
		cfg:        cfg,
		status:     StatusIdle,
		cmdFactory: exec.Command,
	}
	ex.loadHistory()
	return ex
}

func (ex *BackupExecutor) Status() BackupStatus {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	return ex.status
}

func (ex *BackupExecutor) Current() *BackupRun {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if ex.current != nil {
		cp := *ex.current
		return &cp
	}
	return nil
}

func (ex *BackupExecutor) History() []BackupRun {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	out := make([]BackupRun, len(ex.history))
	copy(out, ex.history)
	return out
}

func (ex *BackupExecutor) LastRun() *BackupRun {
	ex.mu.Lock()
	defer ex.mu.Unlock()
	if len(ex.history) == 0 {
		return nil
	}
	cp := ex.history[0]
	return &cp
}

// Run starts a backup. Returns an error if one is already running.
func (ex *BackupExecutor) Run() error {
	ex.mu.Lock()
	if ex.status == StatusRunning {
		ex.mu.Unlock()
		return fmt.Errorf("backup already in progress")
	}
	ex.status = StatusRunning

	runID := time.Now().Format("20060102-150405")
	logFileName := fmt.Sprintf("backup-%s.log", runID)
	logPath := filepath.Join(ex.cfg.LogDir, logFileName)

	run := &BackupRun{
		ID:        runID,
		StartTime: time.Now(),
		Status:    StatusRunning,
		LogFile:   logFileName,
	}
	ex.current = run
	ex.mu.Unlock()

	go ex.execute(run, logPath)
	return nil
}

func (ex *BackupExecutor) execute(run *BackupRun, logPath string) {
	// Ensure log directory exists
	if err := os.MkdirAll(ex.cfg.LogDir, 0755); err != nil {
		log.Printf("failed to create log dir: %v", err)
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		log.Printf("failed to create log file: %v", err)
		ex.finishRun(run, 1, "failed to create log file")
		return
	}
	defer logFile.Close()

	args := ex.buildRsyncArgs()
	cmd := ex.cmdFactory("rsync", args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	fmt.Fprintf(logFile, "=== Backup started at %s ===\n", run.StartTime.Format(time.RFC3339))
	fmt.Fprintf(logFile, "Command: rsync %s\n\n", strings.Join(args, " "))

	err = cmd.Run()

	exitCode := 0
	summary := "completed successfully"
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
		summary = rsyncExitSummary(exitCode)
	}

	fmt.Fprintf(logFile, "\n=== Backup finished at %s (exit code: %d) ===\n",
		time.Now().Format(time.RFC3339), exitCode)

	ex.finishRun(run, exitCode, summary)
	ex.pruneOldLogs()
}

func (ex *BackupExecutor) buildRsyncArgs() []string {
	args := []string{
		"-avz",
		"--delete",
		"--partial",
		"--stats",
		"-e", fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", ex.cfg.SSHKeyPath),
	}

	if ex.cfg.BandwidthLimit > 0 {
		args = append(args, fmt.Sprintf("--bwlimit=%d", ex.cfg.BandwidthLimit))
	}

	var source string
	if ex.cfg.SourceIsFile {
		// Single file: use path as-is, no trailing slash
		source = ex.cfg.SourcePath
	} else {
		// Directory: trailing slash ensures contents are synced, not the directory itself
		source = strings.TrimRight(ex.cfg.SourcePath, "/") + "/"
	}
	dest := fmt.Sprintf("%s:%s/", ex.cfg.RemoteHost, strings.TrimRight(ex.cfg.RemotePath, "/"))

	args = append(args, source, dest)
	return args
}

// rsyncExitSummary returns a human-readable summary for an rsync exit code.
func rsyncExitSummary(code int) string {
	switch code {
	case 1:
		return "syntax or usage error"
	case 2:
		return "protocol incompatibility"
	case 3:
		return "errors selecting input/output files"
	case 5:
		return "error starting client-server protocol"
	case 10:
		return "error in socket I/O"
	case 11:
		return "error in file I/O"
	case 12:
		return "error in rsync protocol data stream"
	case 14:
		return "error in IPC code"
	case 20:
		return "interrupted by signal"
	case 23:
		return "partial transfer — some files could not be transferred"
	case 24:
		return "partial transfer — some source files vanished during sync"
	case 25:
		return "max-delete limit reached"
	case 30:
		return "timeout in data send/receive"
	case 35:
		return "timeout waiting for daemon connection"
	case 255:
		return "SSH connection failed — remote host unreachable or auth denied"
	default:
		return fmt.Sprintf("rsync error (exit code %d)", code)
	}
}

// isPartialTransfer returns true for rsync exit codes that indicate a partial
// but non-fatal transfer (some files skipped, but the rest succeeded).
func isPartialTransfer(exitCode int) bool {
	return exitCode == 23 || exitCode == 24
}

func (ex *BackupExecutor) finishRun(run *BackupRun, exitCode int, summary string) {
	ex.mu.Lock()
	defer ex.mu.Unlock()

	run.EndTime = time.Now()
	run.Duration = run.EndTime.Sub(run.StartTime).Truncate(time.Second).String()
	run.ExitCode = exitCode
	run.Summary = summary

	switch {
	case exitCode == 0:
		run.Status = StatusSuccess
		ex.status = StatusSuccess
	case isPartialTransfer(exitCode):
		run.Status = StatusWarning
		ex.status = StatusWarning
	default:
		run.Status = StatusFailed
		ex.status = StatusFailed
	}

	ex.current = nil

	// Prepend to history (newest first)
	ex.history = append([]BackupRun{*run}, ex.history...)
	if len(ex.history) > 100 {
		ex.history = ex.history[:100]
	}

	ex.saveHistory()
}

func (ex *BackupExecutor) historyPath() string {
	return filepath.Join(ex.cfg.LogDir, "history.json")
}

func (ex *BackupExecutor) loadHistory() {
	data, err := os.ReadFile(ex.historyPath())
	if err != nil {
		return // no history yet
	}
	var runs []BackupRun
	if err := json.Unmarshal(data, &runs); err != nil {
		log.Printf("failed to parse history: %v", err)
		return
	}
	ex.history = runs

	// Set initial status from last run
	if len(ex.history) > 0 {
		ex.status = ex.history[0].Status
	}
}

func (ex *BackupExecutor) saveHistory() {
	data, err := json.MarshalIndent(ex.history, "", "  ")
	if err != nil {
		log.Printf("failed to marshal history: %v", err)
		return
	}
	if err := os.WriteFile(ex.historyPath(), data, 0644); err != nil {
		log.Printf("failed to write history: %v", err)
	}
}

func (ex *BackupExecutor) pruneOldLogs() {
	entries, err := os.ReadDir(ex.cfg.LogDir)
	if err != nil {
		return
	}

	var logFiles []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "backup-") && strings.HasSuffix(e.Name(), ".log") {
			logFiles = append(logFiles, e)
		}
	}

	if len(logFiles) <= ex.cfg.MaxLogFiles {
		return
	}

	// Sort by name (which includes timestamp) ascending
	sort.Slice(logFiles, func(i, j int) bool {
		return logFiles[i].Name() < logFiles[j].Name()
	})

	toRemove := logFiles[:len(logFiles)-ex.cfg.MaxLogFiles]
	for _, f := range toRemove {
		os.Remove(filepath.Join(ex.cfg.LogDir, f.Name()))
	}
}

// CheckRemotePath runs an SSH command to check whether the remote backup
// destination already contains files. Returns true if non-empty.
func (ex *BackupExecutor) CheckRemotePath() (nonEmpty bool, files []string, err error) {
	remotePath := strings.TrimRight(ex.cfg.RemotePath, "/")
	// Parse user@host from RemoteHost
	sshArgs := []string{
		"-i", ex.cfg.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "ConnectTimeout=10",
		ex.cfg.RemoteHost,
		fmt.Sprintf("ls -A '%s/' 2>/dev/null | head -5", remotePath),
	}

	cmd := ex.cmdFactory("ssh", sshArgs...)
	out, err := cmd.Output()
	if err != nil {
		return false, nil, fmt.Errorf("SSH check failed: %w", err)
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return false, nil, nil
	}

	lines := strings.Split(output, "\n")
	return true, lines, nil
}

// ReadLog returns the content of a log file by its filename.
func (ex *BackupExecutor) ReadLog(filename string) (string, error) {
	// Sanitize: only allow filenames, not paths
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
		return "", fmt.Errorf("invalid log filename")
	}
	data, err := os.ReadFile(filepath.Join(ex.cfg.LogDir, filename))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
