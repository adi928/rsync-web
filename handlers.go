package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type Server struct {
	executor  *BackupExecutor
	scheduler *Scheduler
	cfg       *Config
	templates *template.Template
}

func NewServer(cfg *Config, executor *BackupExecutor, scheduler *Scheduler) *Server {
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("2006-01-02 15:04:05")
		},
		"statusClass": func(s BackupStatus) string {
			switch s {
			case StatusSuccess:
				return "success"
			case StatusWarning:
				return "warning"
			case StatusFailed:
				return "failed"
			case StatusRunning:
				return "running"
			default:
				return "idle"
			}
		},
		"timeUntil": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			d := time.Until(t).Truncate(time.Second)
			if d < 0 {
				return "imminent"
			}
			h := int(d.Hours())
			m := int(d.Minutes()) % 60
			s := int(d.Seconds()) % 60
			if h > 0 {
				return fmt.Sprintf("%dh %dm %ds", h, m, s)
			}
			if m > 0 {
				return fmt.Sprintf("%dm %ds", m, s)
			}
			return fmt.Sprintf("%ds", s)
		},
	}

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseGlob(
		filepath.Join("templates", "*.html"),
	))

	return &Server{
		executor:  executor,
		scheduler: scheduler,
		cfg:       cfg,
		templates: tmpl,
	}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/backup", s.handleTriggerBackup)
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/api/logs/", s.handleLogs)
	mux.HandleFunc("/api/remote-check", s.handleRemoteCheck)
	mux.HandleFunc("/fragment/status", s.handleStatusFragment)
	mux.HandleFunc("/fragment/history", s.handleHistoryFragment)
	mux.HandleFunc("/fragment/remote-warning", s.handleRemoteWarningFragment)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
}

// --- Page handlers ---

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := s.dashboardData()
	if err := s.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- API handlers ---

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	data := s.dashboardData()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handleTriggerBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := s.executor.Run(); err != nil {
		// If htmx request, return a fragment
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Reswap", "none")
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(err.Error()))
			return
		}
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	// For htmx, trigger a refresh of the status panel
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Trigger", "backup-started")
		s.handleStatusFragment(w, r)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.executor.History())
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	// Extract filename from /api/logs/{filename}
	filename := filepath.Base(r.URL.Path)
	if filename == "" || filename == "." {
		http.Error(w, "log filename required", http.StatusBadRequest)
		return
	}

	content, err := s.executor.ReadLog(filename)
	if err != nil {
		http.Error(w, "log not found", http.StatusNotFound)
		return
	}

	// If htmx request, return just the log content wrapped in a pre tag
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<pre class="log-content">` + template.HTMLEscapeString(content) + `</pre>`))
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(content))
}

func (s *Server) handleRemoteCheck(w http.ResponseWriter, r *http.Request) {
	nonEmpty, files, err := s.executor.CheckRemotePath()

	type result struct {
		NonEmpty bool     `json:"non_empty"`
		Files    []string `json:"files,omitempty"`
		Error    string   `json:"error,omitempty"`
	}

	res := result{NonEmpty: nonEmpty, Files: files}
	if err != nil {
		res.Error = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (s *Server) handleRemoteWarningFragment(w http.ResponseWriter, r *http.Request) {
	// Only check if there's no backup history (first run scenario)
	if len(s.executor.History()) > 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	nonEmpty, files, err := s.executor.CheckRemotePath()
	if err != nil || !nonEmpty {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	preview := strings.Join(files, ", ")
	if len(files) >= 5 {
		preview += ", ..."
	}
	fmt.Fprintf(w, `<div class="status-hint warning-hint" id="remote-warning">`+
		`Remote path already contains files: <strong>%s</strong><br>`+
		`Running a backup with <code>--delete</code> will remove any files at the destination `+
		`that are not present in the source. Make sure this is the correct target.`+
		`</div>`, template.HTMLEscapeString(preview))
}

// --- Fragment handlers (for htmx partial updates) ---

func (s *Server) handleStatusFragment(w http.ResponseWriter, r *http.Request) {
	data := s.dashboardData()
	w.Header().Set("Content-Type", "text/html")
	if err := s.templates.ExecuteTemplate(w, "status-card", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleHistoryFragment(w http.ResponseWriter, r *http.Request) {
	data := s.dashboardData()
	w.Header().Set("Content-Type", "text/html")
	if err := s.templates.ExecuteTemplate(w, "history-table", data); err != nil {
		log.Printf("template error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- Data ---

type DashboardData struct {
	Status   BackupStatus `json:"status"`
	LastRun  *BackupRun   `json:"last_run"`
	NextRun  time.Time    `json:"next_run"`
	History  []BackupRun  `json:"history"`
	Schedule string       `json:"schedule"`
	Source   string       `json:"source"`
	Dest     string       `json:"dest"`
}

func (s *Server) dashboardData() DashboardData {
	last := s.executor.LastRun()
	history := s.executor.History()
	current := s.executor.Current()

	status := s.executor.Status()
	if current != nil {
		status = StatusRunning
	}

	return DashboardData{
		Status:   status,
		LastRun:  last,
		NextRun:  s.scheduler.NextRun(),
		History:  history,
		Schedule: s.cfg.Schedule,
		Source:   s.cfg.SourcePath,
		Dest:     s.cfg.RemoteHost + ":" + s.cfg.RemotePath,
	}
}
