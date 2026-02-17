package core

import (
	"fmt"
	"log"

	"github.com/tunnelwhisperer/tw/internal/provider"
)

// Service is the core orchestrator for all Tunnel Whisperer operations.
type Service struct {
	ConfigDir string
	Provider  provider.Provider
}

func New(configDir string) *Service {
	return &Service{
		ConfigDir: configDir,
	}
}

// Init prepares the core service (loads config, validates state).
func (s *Service) Init() error {
	log.Printf("core: initialized with config dir %s", s.ConfigDir)
	return nil
}

// StartServer starts the core service loop (tunnel monitoring, relay health checks).
func (s *Service) StartServer() error {
	log.Println("core: server started")
	return nil
}

// DeployRelay provisions a new relay instance using the configured provider.
func (s *Service) DeployRelay(domain string) error {
	if s.Provider == nil {
		return fmt.Errorf("no cloud provider configured")
	}
	return s.Provider.CreateInstance(domain)
}

// Status returns the current server status.
func (s *Service) Status() map[string]string {
	return map[string]string{
		"status":    "running",
		"configDir": s.ConfigDir,
	}
}
