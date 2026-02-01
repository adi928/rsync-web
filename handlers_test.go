package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testServer creates a Server wired up with a fake executor and scheduler,
// ready for use in HTTP tests. It writes a minimal template so template
// parsing does not fail.
func testServer(t *testing.T) (*Server, *BackupExecutor) {
	t.Helper()

	cfg := testConfig(t)
	os.MkdirAll(cfg.LogDir, 0755)

	executor := NewBackupExecutor(cfg)
	executor.cmdFactory = fakeRsyncCmd(0, "ok")

	sched, err := NewScheduler(executor, cfg.Schedule)
	if err != nil {
		t.Fatalf("creating scheduler: %v", err)
	}
	sched.Start()
	t.Cleanup(func() { sched.Stop() })

	// Build templates in-memory rather than depending on the templates/ directory
	funcMap := template.FuncMap{
		"formatTime": func(ti time.Time) string {
			if ti.IsZero() {
				return "—"
			}
			return ti.Format("2006-01-02 15:04:05")
		},
		"statusClass": func(s BackupStatus) string { return string(s) },
		"timeUntil":   func(ti time.Time) string { return "soon" },
	}

	const tmplText = `
{{define "index.html"}}
<html><body>
<div id="status">{{.Status}}</div>
<div id="source">{{.Source}}</div>
<div id="dest">{{.Dest}}</div>
<div id="configured">{{.Configured}}</div>
</body></html>
{{end}}

{{define "status-card"}}
<div class="status-card"><span class="badge">{{.Status}}</span></div>
{{end}}

{{define "history-table"}}
<div id="history">{{range .History}}<div>{{.ID}}</div>{{end}}</div>
{{end}}

{{define "settings-form"}}
<div id="settings-form"><input name="source_path" value="{{.Settings.SourcePath}}"></div>
{{end}}
`

	tmpl := template.Must(template.New("").Funcs(funcMap).Parse(tmplText))

	srv := &Server{
		executor:  executor,
		scheduler: sched,
		cfg:       cfg,
		templates: tmpl,
	}

	return srv, executor
}

func TestHandler_Dashboard(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET / status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "idle") {
		t.Errorf("dashboard should show idle status, body: %s", body)
	}
}

func TestHandler_Dashboard_NotFound(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /nonexistent status = %d, want 404", w.Code)
	}
}

func TestHandler_APIStatus(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/status = %d, want 200", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var data DashboardData
	if err := json.NewDecoder(w.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if data.Status != StatusIdle {
		t.Errorf("status = %q, want idle", data.Status)
	}
	if data.Source != "/mnt/plex-media" {
		t.Errorf("source = %q, want /mnt/plex-media", data.Source)
	}
}

func TestHandler_TriggerBackup(t *testing.T) {
	srv, executor := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// POST should trigger a backup
	req := httptest.NewRequest("POST", "/api/backup", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("POST /api/backup status = %d, want 303 redirect", w.Code)
	}

	// Wait for backup to complete
	if err := waitForStatus(executor, StatusSuccess, 10*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestHandler_TriggerBackup_MethodNotAllowed(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/backup", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/backup status = %d, want 405", w.Code)
	}
}

func TestHandler_TriggerBackup_Conflict(t *testing.T) {
	srv, executor := testServer(t)
	// Make the backup slow so it's still running
	executor.cmdFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sleep", "5")
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Start first backup
	req1 := httptest.NewRequest("POST", "/api/backup", nil)
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	// Wait until running
	if err := waitForStatus(executor, StatusRunning, 5*time.Second); err != nil {
		t.Fatal(err)
	}

	// Try second backup — should return 409
	req2 := httptest.NewRequest("POST", "/api/backup", nil)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Errorf("second POST /api/backup status = %d, want 409", w2.Code)
	}
}

func TestHandler_TriggerBackup_Htmx(t *testing.T) {
	srv, executor := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// htmx POST
	req := httptest.NewRequest("POST", "/api/backup", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("htmx POST /api/backup status = %d, want 200", w.Code)
	}

	if w.Header().Get("HX-Trigger") != "backup-started" {
		t.Errorf("expected HX-Trigger header, got: %q", w.Header().Get("HX-Trigger"))
	}

	if err := waitForStatus(executor, StatusSuccess, 10*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestHandler_APIHistory(t *testing.T) {
	srv, executor := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Run a backup so there's history
	executor.Run()
	waitForStatus(executor, StatusSuccess, 10*time.Second)

	req := httptest.NewRequest("GET", "/api/history", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/history status = %d, want 200", w.Code)
	}

	var history []BackupRun
	if err := json.NewDecoder(w.Body).Decode(&history); err != nil {
		t.Fatalf("failed to decode history: %v", err)
	}
	if len(history) < 1 {
		t.Error("expected at least 1 history entry")
	}
}

func TestHandler_APILogs(t *testing.T) {
	srv, executor := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Run a backup to create a log file
	executor.Run()
	waitForStatus(executor, StatusSuccess, 10*time.Second)

	last := executor.LastRun()
	if last == nil {
		t.Fatal("expected a history entry")
	}

	req := httptest.NewRequest("GET", "/api/logs/"+last.LogFile, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/logs/%s status = %d, want 200", last.LogFile, w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Backup started at") {
		t.Errorf("log should contain 'Backup started at', got: %s", body)
	}
}

func TestHandler_APILogs_NotFound(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/logs/nonexistent.log", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/logs/nonexistent.log status = %d, want 404", w.Code)
	}
}

func TestHandler_APILogs_Htmx(t *testing.T) {
	srv, executor := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Create a log file
	os.WriteFile(filepath.Join(executor.cfg.LogDir, "test.log"), []byte("rsync log data"), 0644)

	req := httptest.NewRequest("GET", "/api/logs/test.log", nil)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("htmx response Content-Type = %q, want text/html", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "<pre") {
		t.Errorf("htmx response should wrap in <pre>, got: %s", body)
	}
	if !strings.Contains(body, "rsync log data") {
		t.Errorf("htmx response should contain log data, got: %s", body)
	}
}

func TestHandler_StatusFragment(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/fragment/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /fragment/status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "status-card") {
		t.Errorf("fragment should contain status-card class, got: %s", body)
	}
}

func TestHandler_HistoryFragment(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/fragment/history", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /fragment/history = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "history") {
		t.Errorf("fragment should contain history div, got: %s", body)
	}
}

func TestHandler_RemoteCheck_NonEmpty(t *testing.T) {
	srv, executor := testServer(t)
	// Fake SSH that returns file listing
	executor.cmdFactory = func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_TEST_PROCESS=1",
			"GO_TEST_EXIT_CODE=0",
			"GO_TEST_OUTPUT=movies\ntv-shows",
		)
		return cmd
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/remote-check", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/remote-check status = %d, want 200", w.Code)
	}

	var result struct {
		NonEmpty bool     `json:"non_empty"`
		Files    []string `json:"files"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
	if !result.NonEmpty {
		t.Error("expected non_empty to be true")
	}
	if len(result.Files) != 2 {
		t.Errorf("expected 2 files, got %d", len(result.Files))
	}
}

func TestHandler_RemoteCheck_Empty(t *testing.T) {
	srv, executor := testServer(t)
	executor.cmdFactory = func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_TEST_PROCESS=1",
			"GO_TEST_EXIT_CODE=0",
			"GO_TEST_OUTPUT=",
		)
		return cmd
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/remote-check", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var result struct {
		NonEmpty bool `json:"non_empty"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if result.NonEmpty {
		t.Error("expected non_empty to be false for empty remote")
	}
}

func TestHandler_RemoteWarningFragment_NoHistory(t *testing.T) {
	srv, executor := testServer(t)
	// Fake SSH returning files — simulates non-empty remote
	executor.cmdFactory = func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_TEST_PROCESS=1",
			"GO_TEST_EXIT_CODE=0",
			"GO_TEST_OUTPUT=movies\ntv-shows",
		)
		return cmd
	}

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/fragment/remote-warning", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "already contains files") {
		t.Errorf("expected warning about existing files, got: %s", body)
	}
	if !strings.Contains(body, "--delete") {
		t.Errorf("expected --delete warning, got: %s", body)
	}
}

func TestHandler_RemoteWarningFragment_WithHistory(t *testing.T) {
	srv, executor := testServer(t)

	// Run a backup so there's history
	executor.Run()
	waitForStatus(executor, StatusSuccess, 10*time.Second)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/fragment/remote-warning", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	// Should return empty body — no warning when history exists
	body := w.Body.String()
	if strings.Contains(body, "already contains files") {
		t.Error("should not show warning when backup history exists")
	}
}

func TestHandler_APIStatus_Configured(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var data DashboardData
	json.NewDecoder(w.Body).Decode(&data)

	if !data.Configured {
		t.Error("expected Configured=true when testConfig has all transfer fields")
	}
}

func TestHandler_Settings_GET(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /api/settings status = %d, want 200", w.Code)
	}

	var settings TransferSettings
	if err := json.NewDecoder(w.Body).Decode(&settings); err != nil {
		t.Fatalf("failed to decode settings: %v", err)
	}
	if settings.SourcePath != "/mnt/plex-media" {
		t.Errorf("source_path = %q, want /mnt/plex-media", settings.SourcePath)
	}
}

func TestHandler_Settings_POST(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := strings.NewReader("source_path=/data&remote_host=user@host&remote_path=/backup&ssh_key_path=~/.ssh/key")
	req := httptest.NewRequest("POST", "/api/settings", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("POST /api/settings status = %d, want 303", w.Code)
	}

	// Verify config was updated
	if srv.cfg.SourcePath != "/data" {
		t.Errorf("cfg.SourcePath = %q, want /data", srv.cfg.SourcePath)
	}
	if srv.cfg.RemoteHost != "user@host" {
		t.Errorf("cfg.RemoteHost = %q, want user@host", srv.cfg.RemoteHost)
	}
}

func TestHandler_Settings_POST_MissingFields(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := strings.NewReader("source_path=/data")
	req := httptest.NewRequest("POST", "/api/settings", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("POST /api/settings with missing fields status = %d, want 400", w.Code)
	}
}

func TestHandler_Settings_MethodNotAllowed(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/api/settings", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/settings status = %d, want 405", w.Code)
	}
}

func TestHandler_SettingsFragment(t *testing.T) {
	srv, _ := testServer(t)

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/fragment/settings", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET /fragment/settings = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "settings-form") {
		t.Errorf("fragment should contain settings-form, got: %s", body)
	}
}
