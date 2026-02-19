package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config holds all Tunnel Whisperer settings.
type Config struct {
	SSH       SSHConfig       `yaml:"ssh"`
	API       APIConfig       `yaml:"api"`
	Dashboard DashboardConfig `yaml:"dashboard"`
	Relay     RelayConfig     `yaml:"relay"`
	Xray      XrayConfig      `yaml:"xray"`
	Client    ClientConfig    `yaml:"client"`
}

type SSHConfig struct {
	Port           int    `yaml:"port"`
	HostKeyDir     string `yaml:"host_key_dir"`
	AuthorizedKeys string `yaml:"authorized_keys"`
}

type APIConfig struct {
	Port int `yaml:"port"`
}

type DashboardConfig struct {
	Port int `yaml:"port"`
}

type RelayConfig struct {
	Provider string `yaml:"provider"`
	Domain   string `yaml:"domain"`
	Region   string `yaml:"region"`
}

type XrayConfig struct {
	Enabled      bool   `yaml:"enabled"`
	UUID         string `yaml:"uuid"`
	RelayHost    string `yaml:"relay_host"`
	RelayPort    int    `yaml:"relay_port"`
	Path         string `yaml:"path"`
	ListenPort   int    `yaml:"listen_port"`
	RelaySSHPort int    `yaml:"relay_ssh_port"`
	RelaySSHUser string `yaml:"relay_ssh_user"`
	RemotePort   int    `yaml:"remote_port"`
}

type ClientConfig struct {
	SSHUser        string `yaml:"ssh_user"`
	LocalPort      int    `yaml:"local_port"`
	RemoteHost     string `yaml:"remote_host"`
	RemotePort     int    `yaml:"remote_port"`
	XrayListenPort int    `yaml:"xray_listen_port"`
	ServerSSHPort  int    `yaml:"server_ssh_port"`
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		SSH: SSHConfig{
			Port:           2222,
			HostKeyDir:     Dir(),
			AuthorizedKeys: filepath.Join(Dir(), "authorized_keys"),
		},
		API: APIConfig{
			Port: 50051,
		},
		Dashboard: DashboardConfig{
			Port: 8080,
		},
		Relay: RelayConfig{
			Provider: "aws",
		},
		Xray: XrayConfig{
			Enabled:      false,
			RelayPort:    443,
			Path:         "/tw",
			RelaySSHPort: 22,
			RelaySSHUser: "ubuntu",
			RemotePort:   2222,
		},
		Client: ClientConfig{
			SSHUser:        "tunnel",
			LocalPort:      53389,
			RemoteHost:     "localhost",
			RemotePort:     3389,
			XrayListenPort: 54001,
			ServerSSHPort:  2222,
		},
	}
}

// Dir returns the platform-specific config directory.
//
//	Linux:   /etc/tw/config
//	Windows: C:\ProgramData\tw\config
//
// Override with TW_CONFIG_DIR environment variable.
func Dir() string {
	if d := os.Getenv("TW_CONFIG_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		return `C:\ProgramData\tw\config`
	}
	return "/etc/tw/config"
}

// FilePath returns the full path to the config file.
func FilePath() string {
	return filepath.Join(Dir(), "config.yaml")
}

// Load reads the YAML config file from the platform-specific path.
// If the file does not exist, it returns the default configuration.
func Load() (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(FilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// Save writes the configuration to the platform-specific YAML file.
func Save(cfg *Config) error {
	if err := os.MkdirAll(Dir(), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(FilePath(), data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}
