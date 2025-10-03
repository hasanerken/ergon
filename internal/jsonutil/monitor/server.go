package monitor

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hasanerken/ergon"
)

//go:embed templates/* static/*
var embedFS embed.FS

// Server provides a web UI for monitoring and managing tasks
type Server struct {
	manager    *ergon.Manager
	templates  *template.Template
	httpServer *http.Server
}

// Config configures the monitor server
type Config struct {
	Addr     string // Listen address (default: ":8080")
	BasePath string // Base URL path (default: "/monitor")
}

// NewServer creates a new monitoring server
func NewServer(manager *ergon.Manager, cfg Config) (*Server, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.BasePath == "" {
		cfg.BasePath = "/monitor"
	}

	// Parse templates with custom functions
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Format("2006-01-02 15:04:05")
		},
		"formatDuration": func(d time.Duration) string {
			if d == 0 {
				return "-"
			}
			return d.String()
		},
		"stateClass": func(state ergon.TaskState) string {
			switch state {
			case ergon.StatePending, ergon.StateScheduled:
				return "bg-blue-100 text-blue-800"
			case ergon.StateRunning:
				return "bg-yellow-100 text-yellow-800"
			case ergon.StateCompleted:
				return "bg-green-100 text-green-800"
			case ergon.StateFailed:
				return "bg-red-100 text-red-800"
			case ergon.StateCancelled:
				return "bg-gray-100 text-gray-800"
			case ergon.StateRetrying:
				return "bg-orange-100 text-orange-800"
			default:
				return "bg-gray-100 text-gray-800"
			}
		},
		"truncate": func(s string, length int) string {
			if len(s) <= length {
				return s
			}
			return s[:length] + "..."
		},
		"formatJSON": func(data []byte) string {
			if len(data) == 0 {
				return "{}"
			}
			var v interface{}
			if err := json.Unmarshal(data, &v); err != nil {
				return string(data)
			}
			formatted, _ := json.MarshalIndent(v, "", "  ")
			return string(formatted)
		},
		"add": func(a, b int) int {
			return a + b
		},
		"urlencoded": func(filter *ergon.TaskFilter) string {
			// Simple URL encoding for filter parameters
			params := ""
			if filter.State != "" {
				params += "state=" + string(filter.State) + "&"
			}
			if filter.Queue != "" {
				params += "queue=" + filter.Queue + "&"
			}
			if filter.Kind != "" {
				params += "kind=" + filter.Kind + "&"
			}
			return params
		},
	}).ParseFS(embedFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	s := &Server{
		manager:   manager,
		templates: tmpl,
	}

	// Setup HTTP routes
	mux := http.NewServeMux()

	// Serve static files
	mux.Handle(cfg.BasePath+"/static/", http.StripPrefix(cfg.BasePath+"/static/",
		http.FileServer(http.FS(embedFS))))

	// Page routes
	mux.HandleFunc(cfg.BasePath+"/", s.handleTaskList)
	mux.HandleFunc(cfg.BasePath+"/tasks", s.handleTaskList)
	mux.HandleFunc(cfg.BasePath+"/tasks/", s.handleTaskDetail)
	mux.HandleFunc(cfg.BasePath+"/queues", s.handleQueueList)
	mux.HandleFunc(cfg.BasePath+"/dashboard", s.handleDashboard)

	// API routes for HTMX
	mux.HandleFunc(cfg.BasePath+"/api/tasks", s.handleTasksAPI)
	mux.HandleFunc(cfg.BasePath+"/api/tasks/cancel", s.handleCancelTask)
	mux.HandleFunc(cfg.BasePath+"/api/tasks/delete", s.handleDeleteTask)
	mux.HandleFunc(cfg.BasePath+"/api/tasks/retry", s.handleRetryTask)
	mux.HandleFunc(cfg.BasePath+"/api/tasks/reschedule", s.handleRescheduleTask)
	mux.HandleFunc(cfg.BasePath+"/api/stats", s.handleStatsAPI)

	s.httpServer = &http.Server{
		Addr:    cfg.Addr,
		Handler: mux,
	}

	return s, nil
}

// Start starts the monitoring server
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Stop stops the monitoring server
func (s *Server) Stop() error {
	return s.httpServer.Close()
}

// handleDashboard renders the main dashboard
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := s.manager.GetOverallStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	queues, err := s.manager.ListQueues(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Stats":  stats,
		"Queues": queues,
	}

	s.templates.ExecuteTemplate(w, "dashboard.html", data)
}

// handleTaskList renders the task list page
func (s *Server) handleTaskList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	filter := &ergon.TaskFilter{
		Queue: query.Get("queue"),
		Kind:  query.Get("kind"),
		Limit: 50,
	}

	if state := query.Get("state"); state != "" {
		s := ergon.TaskState(state)
		filter.State = s
	}

	if offset := query.Get("offset"); offset != "" {
		filter.Offset, _ = strconv.Atoi(offset)
	}

	tasks, err := s.manager.ListTasks(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	count, err := s.manager.CountTasks(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get all queues for filter dropdown
	queues, err := s.manager.ListQueues(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get unique task kinds
	kinds := s.getUniqueKinds(r.Context())

	// Get state counts for tabs
	stateCounts := s.getStateCounts(r.Context(), filter.Queue, filter.Kind)

	data := map[string]interface{}{
		"Tasks":       tasks,
		"Filter":      filter,
		"Count":       count,
		"Offset":      filter.Offset,
		"HasMore":     count > filter.Offset+filter.Limit,
		"Queues":      queues,
		"Kinds":       kinds,
		"StateCounts": stateCounts,
	}

	// If HTMX request, return only task rows
	if r.Header.Get("HX-Request") == "true" {
		if err := s.templates.ExecuteTemplate(w, "task-rows.html", data); err != nil {
			http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if err := s.templates.ExecuteTemplate(w, "tasks.html", data); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// getUniqueKinds gets all unique task kinds from the system
func (s *Server) getUniqueKinds(ctx context.Context) []string {
	// Get a sample of tasks to extract kinds
	tasks, err := s.manager.ListTasks(ctx, &ergon.TaskFilter{Limit: 1000})
	if err != nil {
		return []string{}
	}

	kindMap := make(map[string]bool)
	for _, task := range tasks {
		kindMap[task.Kind] = true
	}

	kinds := make([]string, 0, len(kindMap))
	for kind := range kindMap {
		kinds = append(kinds, kind)
	}
	return kinds
}

// getStateCounts gets task counts per state
func (s *Server) getStateCounts(ctx context.Context, queue, kind string) map[ergon.TaskState]int {
	states := []ergon.TaskState{
		ergon.StatePending,
		ergon.StateScheduled,
		ergon.StateAvailable,
		ergon.StateRunning,
		ergon.StateCompleted,
		ergon.StateFailed,
		ergon.StateCancelled,
		ergon.StateRetrying,
	}

	counts := make(map[ergon.TaskState]int)
	for _, state := range states {
		filter := &ergon.TaskFilter{
			Queue: queue,
			Kind:  kind,
			State: state,
		}
		count, err := s.manager.CountTasks(ctx, filter)
		if err == nil {
			counts[state] = count
		}
	}
	return counts
}

// handleTaskDetail renders task detail for modal
func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/monitor/tasks/")

	taskDetails, err := s.manager.GetTaskDetails(r.Context(), taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Return modal content only
	if err := s.templates.ExecuteTemplate(w, "task-modal", taskDetails); err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// handleQueueList renders the queue list page
func (s *Server) handleQueueList(w http.ResponseWriter, r *http.Request) {
	queues, err := s.manager.ListQueues(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Queues": queues,
	}

	s.templates.ExecuteTemplate(w, "queues.html", data)
}

// API Handlers

func (s *Server) handleTasksAPI(w http.ResponseWriter, r *http.Request) {
	// Same as handleTaskList but always returns JSON
	query := r.URL.Query()

	filter := &ergon.TaskFilter{
		Queue: query.Get("queue"),
		Kind:  query.Get("kind"),
		Limit: 50,
	}

	if state := query.Get("state"); state != "" {
		s := ergon.TaskState(state)
		filter.State = s
	}

	tasks, err := s.manager.ListTasks(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	if taskID == "" {
		http.Error(w, "task_id required", http.StatusBadRequest)
		return
	}

	if err := s.manager.Cancel(r.Context(), taskID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return success message for HTMX
	w.Header().Set("HX-Trigger", "taskUpdated")
	fmt.Fprintf(w, `<div class="p-4 bg-green-100 text-green-800 rounded">Task cancelled successfully</div>`)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	if taskID == "" {
		http.Error(w, "task_id required", http.StatusBadRequest)
		return
	}

	if err := s.manager.Delete(r.Context(), taskID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "taskDeleted")
	fmt.Fprintf(w, `<div class="p-4 bg-green-100 text-green-800 rounded">Task deleted successfully</div>`)
}

func (s *Server) handleRetryTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	if taskID == "" {
		http.Error(w, "task_id required", http.StatusBadRequest)
		return
	}

	if err := s.manager.RetryNow(r.Context(), taskID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "taskUpdated")
	fmt.Fprintf(w, `<div class="p-4 bg-green-100 text-green-800 rounded">Task queued for retry</div>`)
}

func (s *Server) handleRescheduleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	taskID := r.FormValue("task_id")
	scheduleStr := r.FormValue("scheduled_at")

	if taskID == "" || scheduleStr == "" {
		http.Error(w, "task_id and scheduled_at required", http.StatusBadRequest)
		return
	}

	scheduledAt, err := time.Parse(time.RFC3339, scheduleStr)
	if err != nil {
		http.Error(w, "invalid scheduled_at format", http.StatusBadRequest)
		return
	}

	if err := s.manager.Reschedule(r.Context(), taskID, scheduledAt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "taskUpdated")
	fmt.Fprintf(w, `<div class="p-4 bg-green-100 text-green-800 rounded">Task rescheduled successfully</div>`)
}

func (s *Server) handleStatsAPI(w http.ResponseWriter, r *http.Request) {
	stats, err := s.manager.GetOverallStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
