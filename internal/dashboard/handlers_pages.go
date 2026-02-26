package dashboard

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/ops"
	"gopkg.in/yaml.v3"
)

func cappedUsers(users []ops.UserInfo, max int) []ops.UserInfo {
	if len(users) <= max {
		return users
	}
	return users[:max]
}

func (s *Server) renderPage(w http.ResponseWriter, page string, data interface{}) {
	tmpl, ok := s.pages[page]
	if !ok {
		slog.Error("template not found", "page", page)
		http.Error(w, "page not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		slog.Error("template render error", "page", page, "error", err)
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	mode := s.ops.Mode()

	// No mode chosen yet — show mode selection.
	if mode == "" {
		s.renderPage(w, "setup", struct {
			pageData
		}{
			pageData: pageData{Title: "Setup", Active: "index", Mode: mode},
		})
		return
	}

	cfg := s.ops.Config()
	relay := s.ops.GetRelayStatus()
	users, _ := s.ops.ListUsers()
	srvStatus := s.ops.ServerStatus()
	cliStatus := s.ops.ClientStatus()

	data := struct {
		pageData
		Config       *config.Config
		ConfigPath   string
		Relay        ops.RelayStatus
		UserCount    int
		Users        []ops.UserInfo
		ServerStatus ops.ServerStatus
		ClientStatus ops.ClientStatus
	}{
		pageData:     pageData{Title: "Status", Active: "index", Mode: mode},
		Config:       cfg,
		ConfigPath:   config.FilePath(),
		Relay:        relay,
		UserCount:    len(users),
		Users:        cappedUsers(users, 3),
		ServerStatus: srvStatus,
		ClientStatus: cliStatus,
	}
	s.renderPage(w, "index", data)
}

func (s *Server) handleRelay(w http.ResponseWriter, r *http.Request) {
	relay := s.ops.GetRelayStatus()
	mode := s.ops.Mode()

	data := struct {
		pageData
		Relay ops.RelayStatus
	}{
		pageData: pageData{Title: "Relay", Active: "relay", Mode: mode},
		Relay:    relay,
	}
	s.renderPage(w, "relay", data)
}

func (s *Server) handleRelayWizard(w http.ResponseWriter, r *http.Request) {
	cfg := s.ops.Config()
	providers := ops.CloudProviders()
	providersJSON, _ := json.Marshal(providers)
	mode := s.ops.Mode()

	data := struct {
		pageData
		Config        *config.Config
		ProvidersJSON template.JS
	}{
		pageData:      pageData{Title: "Provision Relay", Active: "relay", Mode: mode},
		Config:        cfg,
		ProvidersJSON: template.JS(providersJSON),
	}
	s.renderPage(w, "relay_wizard", data)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.ops.ListUsers()
	if err != nil {
		slog.Error("listing users", "error", err)
	}
	mode := s.ops.Mode()
	relay := s.ops.GetRelayStatus()
	srvStatus := s.ops.ServerStatus()

	var inactiveCount int
	for _, u := range users {
		if !u.Active {
			inactiveCount++
		}
	}

	data := struct {
		pageData
		Users         []ops.UserInfo
		RelayReady    bool
		ServerRunning bool
		InactiveCount int
	}{
		pageData:      pageData{Title: "Users", Active: "users", Mode: mode},
		Users:         users,
		RelayReady:    relay.Provisioned,
		ServerRunning: string(srvStatus.State) == "running",
		InactiveCount: inactiveCount,
	}
	s.renderPage(w, "users", data)
}

func (s *Server) handleUserNew(w http.ResponseWriter, r *http.Request) {
	mode := s.ops.Mode()
	relay := s.ops.GetRelayStatus()
	srvStatus := s.ops.ServerStatus()

	data := struct {
		pageData
		RelayReady    bool
		ServerRunning bool
	}{
		pageData:      pageData{Title: "Create User", Active: "users", Mode: mode},
		RelayReady:    relay.Provisioned,
		ServerRunning: string(srvStatus.State) == "running",
	}
	s.renderPage(w, "user_new", data)
}

func (s *Server) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/users/")
	if name == "" || name == "new" {
		http.NotFound(w, r)
		return
	}

	users, _ := s.ops.ListUsers()
	var found *ops.UserInfo
	for _, u := range users {
		if u.Name == name {
			found = &u
			break
		}
	}

	if found == nil {
		http.NotFound(w, r)
		return
	}

	mode := s.ops.Mode()
	data := struct {
		pageData
		User ops.UserInfo
	}{
		pageData: pageData{Title: "User: " + name, Active: "users", Mode: mode},
		User:     *found,
	}
	s.renderPage(w, "user_detail", data)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.ops.Config()
	cfgYAML, _ := yaml.Marshal(cfg)
	mode := s.ops.Mode()

	data := struct {
		pageData
		ConfigPath string
		ConfigYAML string
	}{
		pageData:   pageData{Title: "Config", Active: "config", Mode: mode},
		ConfigPath: config.FilePath(),
		ConfigYAML: string(cfgYAML),
	}
	s.renderPage(w, "config", data)
}
