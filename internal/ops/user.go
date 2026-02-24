package ops

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tunnelwhisperer/tw/internal/config"
	twssh "github.com/tunnelwhisperer/tw/internal/ssh"
	twxray "github.com/tunnelwhisperer/tw/internal/xray"
	gossh "golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

// UserInfo describes one user.
type UserInfo struct {
	Name     string          `json:"name"`
	UUID     string          `json:"uuid,omitempty"`
	Tunnels  []config.Tunnel `json:"tunnels,omitempty"`
	HasKey   bool            `json:"has_key"`
	DirPath  string          `json:"-"`
}

// PortMapping defines one client-port → server-port pair.
type PortMapping struct {
	ClientPort int `json:"client_port"`
	ServerPort int `json:"server_port"`
}

// CreateUserRequest holds the parameters for creating a new user.
type CreateUserRequest struct {
	Name     string        `json:"name"`
	Mappings []PortMapping `json:"mappings"`
}

// ListUsers returns all users found in the users directory.
func (o *Ops) ListUsers() ([]UserInfo, error) {
	usersDir := config.UsersDir()
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var users []UserInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ui := UserInfo{
			Name:    e.Name(),
			DirPath: filepath.Join(usersDir, e.Name()),
		}

		// Try to read the client config.
		cfgPath := filepath.Join(ui.DirPath, "config.yaml")
		if data, err := os.ReadFile(cfgPath); err == nil {
			var clientCfg struct {
				Xray   config.XrayConfig   `yaml:"xray"`
				Client config.ClientConfig `yaml:"client"`
			}
			if yaml.Unmarshal(data, &clientCfg) == nil {
				ui.UUID = clientCfg.Xray.UUID
				ui.Tunnels = clientCfg.Client.Tunnels
			}
		}

		if _, err := os.Stat(filepath.Join(ui.DirPath, "id_ed25519")); err == nil {
			ui.HasKey = true
		}

		users = append(users, ui)
	}
	return users, nil
}

// CreateUser runs the user creation flow: generates credentials, updates the
// relay, saves config, and updates authorized_keys.
func (o *Ops) CreateUser(ctx context.Context, req CreateUserRequest, progress ProgressFunc) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if progress == nil {
		progress = func(ProgressEvent) {}
	}

	cfg := o.cfg

	// Validate.
	if req.Name == "" {
		return fmt.Errorf("user name is required")
	}
	for _, r := range req.Name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("user name must contain only letters, numbers, dashes, and underscores")
		}
	}
	if len(req.Mappings) == 0 {
		return fmt.Errorf("at least one port mapping is required")
	}
	if cfg.Xray.RelayHost == "" {
		return fmt.Errorf("xray.relay_host must be configured before creating users")
	}
	if cfg.Xray.UUID == "" {
		return fmt.Errorf("server UUID must be set — run `tw serve` or `tw create relay-server` first")
	}

	userDir := filepath.Join(config.UsersDir(), req.Name)
	if _, err := os.Stat(userDir); err == nil {
		return fmt.Errorf("user %q already exists", req.Name)
	}

	// Step 1: Generate credentials.
	progress(ProgressEvent{Step: 1, Total: 4, Label: "Generating credentials", Status: "running"})
	clientUUID := uuid.New().String()
	privPEM, pubAuthorized, err := twssh.GenerateKeyPair()
	if err != nil {
		progress(ProgressEvent{Step: 1, Total: 4, Label: "Generating credentials", Status: "failed", Error: err.Error()})
		return fmt.Errorf("generating SSH key pair: %w", err)
	}
	progress(ProgressEvent{Step: 1, Total: 4, Label: "Generating credentials", Status: "completed", Message: "UUID: " + clientUUID})

	// Step 2: Update relay.
	progress(ProgressEvent{Step: 2, Total: 4, Label: "Updating relay", Status: "running"})
	if err := addUUIDToRelay(cfg, clientUUID); err != nil {
		slog.Warn("relay update failed", "error", err)
		progress(ProgressEvent{Step: 2, Total: 4, Label: "Updating relay", Status: "completed", Message: "Warning: " + err.Error()})
	} else {
		progress(ProgressEvent{Step: 2, Total: 4, Label: "Updating relay", Status: "completed", Message: "UUID added to relay"})
	}

	// Step 3: Save user files.
	progress(ProgressEvent{Step: 3, Total: 4, Label: "Saving configuration", Status: "running"})

	if err := os.MkdirAll(userDir, 0700); err != nil {
		progress(ProgressEvent{Step: 3, Total: 4, Label: "Saving configuration", Status: "failed", Error: err.Error()})
		return fmt.Errorf("creating user directory: %w", err)
	}

	if err := os.WriteFile(filepath.Join(userDir, "id_ed25519"), privPEM, 0600); err != nil {
		progress(ProgressEvent{Step: 3, Total: 4, Label: "Saving configuration", Status: "failed", Error: err.Error()})
		return fmt.Errorf("writing client private key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "id_ed25519.pub"), pubAuthorized, 0644); err != nil {
		progress(ProgressEvent{Step: 3, Total: 4, Label: "Saving configuration", Status: "failed", Error: err.Error()})
		return fmt.Errorf("writing client public key: %w", err)
	}

	tunnels := make([]config.Tunnel, len(req.Mappings))
	serverPorts := make([]int, len(req.Mappings))
	for i, m := range req.Mappings {
		tunnels[i] = config.Tunnel{
			LocalPort:  m.ClientPort,
			RemoteHost: "127.0.0.1",
			RemotePort: m.ServerPort,
		}
		serverPorts[i] = m.ServerPort
	}

	clientCfg := struct {
		Xray   config.XrayConfig   `yaml:"xray"`
		Client config.ClientConfig `yaml:"client"`
	}{
		Xray: config.XrayConfig{
			UUID:      clientUUID,
			RelayHost: cfg.Xray.RelayHost,
			RelayPort: cfg.Xray.RelayPort,
			Path:      cfg.Xray.Path,
		},
		Client: config.ClientConfig{
			SSHUser:       req.Name,
			ServerSSHPort: cfg.Server.RemotePort,
			Tunnels:       tunnels,
		},
	}

	cfgData, err := yaml.Marshal(clientCfg)
	if err != nil {
		progress(ProgressEvent{Step: 3, Total: 4, Label: "Saving configuration", Status: "failed", Error: err.Error()})
		return fmt.Errorf("marshaling client config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), cfgData, 0644); err != nil {
		progress(ProgressEvent{Step: 3, Total: 4, Label: "Saving configuration", Status: "failed", Error: err.Error()})
		return fmt.Errorf("writing client config: %w", err)
	}
	progress(ProgressEvent{Step: 3, Total: 4, Label: "Saving configuration", Status: "completed"})

	// Step 4: Update authorized_keys.
	progress(ProgressEvent{Step: 4, Total: 4, Label: "Updating authorized_keys", Status: "running"})
	if err := appendAuthorizedKey(pubAuthorized, req.Name, serverPorts); err != nil {
		progress(ProgressEvent{Step: 4, Total: 4, Label: "Updating authorized_keys", Status: "failed", Error: err.Error()})
		return fmt.Errorf("updating authorized_keys: %w", err)
	}
	progress(ProgressEvent{Step: 4, Total: 4, Label: "Updating authorized_keys", Status: "completed"})

	return nil
}

// DeleteUser removes a user's UUID from the relay, then removes the user
// directory and their authorized_keys entry.
func (o *Ops) DeleteUser(name string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	userDir := filepath.Join(config.UsersDir(), name)
	if _, err := os.Stat(userDir); os.IsNotExist(err) {
		return fmt.Errorf("user %q not found", name)
	}

	// Read the user's UUID so we can remove it from the relay.
	cfgPath := filepath.Join(userDir, "config.yaml")
	if data, err := os.ReadFile(cfgPath); err == nil {
		var clientCfg struct {
			Xray config.XrayConfig `yaml:"xray"`
		}
		if yaml.Unmarshal(data, &clientCfg) == nil && clientCfg.Xray.UUID != "" {
			if err := removeUUIDFromRelay(o.cfg, clientCfg.Xray.UUID); err != nil {
				slog.Warn("could not remove UUID from relay", "user", name, "error", err)
			}
		}
	}

	// Read the user's public key so we can remove it from authorized_keys.
	pubPath := filepath.Join(userDir, "id_ed25519.pub")
	pubData, _ := os.ReadFile(pubPath)

	// Remove user directory.
	if err := os.RemoveAll(userDir); err != nil {
		return fmt.Errorf("removing user directory: %w", err)
	}

	// Remove from authorized_keys.
	if len(pubData) > 0 {
		if err := removeAuthorizedKey(pubData); err != nil {
			slog.Warn("could not remove authorized_keys entry", "user", name, "error", err)
		}
	}

	return nil
}

// GetUserConfigBundle returns the user's config files as a zip archive.
func (o *Ops) GetUserConfigBundle(name string) ([]byte, error) {
	userDir := filepath.Join(config.UsersDir(), name)
	if _, err := os.Stat(userDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("user %q not found", name)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	files := []string{"config.yaml", "id_ed25519", "id_ed25519.pub"}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(userDir, f))
		if err != nil {
			continue
		}
		w, err := zw.Create(f)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// appendAuthorizedKey adds a public key to the server's authorized_keys
// with permitopen restrictions.
func appendAuthorizedKey(pubKey []byte, comment string, ports []int) error {
	akPath := config.AuthorizedKeysPath()

	var options []string
	for _, port := range ports {
		options = append(options, fmt.Sprintf(`permitopen="127.0.0.1:%d"`, port))
	}

	keyLine := strings.TrimSpace(string(pubKey))
	line := fmt.Sprintf("%s %s %s@tw\n", strings.Join(options, ","), keyLine, comment)

	existing, err := os.ReadFile(akPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading authorized_keys: %w", err)
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing = append(existing, '\n')
	}

	return os.WriteFile(akPath, append(existing, []byte(line)...), 0600)
}

// removeAuthorizedKey removes lines containing the given public key.
func removeAuthorizedKey(pubKey []byte) error {
	akPath := config.AuthorizedKeysPath()
	data, err := os.ReadFile(akPath)
	if err != nil {
		return err
	}

	keyStr := strings.TrimSpace(string(pubKey))
	// The key content (ssh-ed25519 AAAA...) may be wrapped with options;
	// match on the base64 portion.
	parts := strings.Fields(keyStr)
	var matchStr string
	if len(parts) >= 2 {
		matchStr = parts[1] // the base64 key data
	} else {
		matchStr = keyStr
	}

	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		if strings.Contains(line, matchStr) {
			continue // remove this line
		}
		kept = append(kept, line)
	}

	result := strings.Join(kept, "\n")
	if len(kept) > 0 {
		result += "\n"
	}
	return os.WriteFile(akPath, []byte(result), 0600)
}

// withRelaySSH opens a temporary Xray tunnel to the relay, establishes an
// SSH connection, and passes it to fn. The tunnel and connection are torn
// down automatically when fn returns.
func withRelaySSH(cfg *config.Config, fn func(client *gossh.Client) error) error {
	xrayInstance, err := twxray.New(cfg.Xray)
	if err != nil {
		return fmt.Errorf("initializing Xray: %w", err)
	}
	const tempPort = 59000
	if err := xrayInstance.Start(tempPort, cfg.Server.RelaySSHPort); err != nil {
		return fmt.Errorf("starting Xray: %w", err)
	}
	defer xrayInstance.Close()

	privPath := filepath.Join(config.Dir(), "id_ed25519")
	keyData, err := os.ReadFile(privPath)
	if err != nil {
		return fmt.Errorf("reading server key: %w", err)
	}
	signer, err := gossh.ParsePrivateKey(keyData)
	if err != nil {
		return fmt.Errorf("parsing server key: %w", err)
	}

	sshCfg := &gossh.ClientConfig{
		User:            cfg.Server.RelaySSHUser,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	xrayAddr := fmt.Sprintf("127.0.0.1:%d", tempPort+1)

	var client *gossh.Client
	for i := 0; i < 15; i++ {
		client, err = gossh.Dial("tcp", xrayAddr, sshCfg)
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return fmt.Errorf("SSH to relay: %w", err)
	}
	defer client.Close()

	return fn(client)
}

// readRelayXrayConfig reads and parses the Xray config from the relay.
func readRelayXrayConfig(client *gossh.Client) (map[string]interface{}, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	out, err := session.Output("sudo cat /usr/local/etc/xray/config.json")
	session.Close()
	if err != nil {
		return nil, fmt.Errorf("reading relay config: %w", err)
	}

	var xrayConf map[string]interface{}
	if err := json.Unmarshal(out, &xrayConf); err != nil {
		return nil, fmt.Errorf("parsing relay config: %w", err)
	}
	return xrayConf, nil
}

// writeRelayXrayConfig writes the Xray config to the relay for persistence.
// It does NOT restart Xray — use the Xray API for runtime changes.
func writeRelayXrayConfig(client *gossh.Client, xrayConf map[string]interface{}) error {
	updatedJSON, err := json.MarshalIndent(xrayConf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	session.Stdin = bytes.NewReader(updatedJSON)
	err = session.Run("sudo tee /usr/local/etc/xray/config.json > /dev/null")
	session.Close()
	if err != nil {
		return fmt.Errorf("writing relay config: %w", err)
	}

	return nil
}

// restartRelayXray restarts the Xray service on the relay.
// Used as a fallback when the Xray API is not available.
func restartRelayXray(client *gossh.Client) {
	session, err := client.NewSession()
	if err != nil {
		return
	}
	_ = session.Run("sudo systemctl restart xray")
	session.Close()
}

// xrayAPIAddUser adds a user to the running Xray instance via its gRPC API.
func xrayAPIAddUser(client *gossh.Client, uuid string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	payload := fmt.Sprintf(`{"inboundTag":"vless-in","user":{"email":"%s","level":0,"account":{"id":"%s"}}}`, uuid, uuid)
	session.Stdin = strings.NewReader(payload)
	return session.Run("/usr/local/bin/xray api adu --server=127.0.0.1:10085")
}

// xrayAPIRemoveUser removes a user from the running Xray instance via its gRPC API.
func xrayAPIRemoveUser(client *gossh.Client, uuid string) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	payload := fmt.Sprintf(`{"inboundTag":"vless-in","email":"%s"}`, uuid)
	session.Stdin = strings.NewReader(payload)
	return session.Run("/usr/local/bin/xray api rmu --server=127.0.0.1:10085")
}

// relayClients extracts the clients slice from the VLESS inbound in the
// parsed Xray config.  It finds the inbound by tag ("vless-in") or
// protocol ("vless") to avoid depending on array ordering.
func relayClients(xrayConf map[string]interface{}) (settings map[string]interface{}, clients []interface{}, err error) {
	inbounds, _ := xrayConf["inbounds"].([]interface{})
	if len(inbounds) == 0 {
		return nil, nil, fmt.Errorf("no inbounds in relay config")
	}

	var inbound map[string]interface{}
	for _, ib := range inbounds {
		m, ok := ib.(map[string]interface{})
		if !ok {
			continue
		}
		if tag, _ := m["tag"].(string); tag == "vless-in" {
			inbound = m
			break
		}
		if proto, _ := m["protocol"].(string); proto == "vless" {
			inbound = m
			break
		}
	}
	if inbound == nil {
		return nil, nil, fmt.Errorf("no VLESS inbound in relay config")
	}

	settings, _ = inbound["settings"].(map[string]interface{})
	clients, _ = settings["clients"].([]interface{})
	return settings, clients, nil
}

// addUUIDToRelay connects to the relay via a temporary Xray tunnel and
// adds a new client UUID to the relay's Xray config.  It first persists
// the change to disk, then tries the Xray gRPC API so the running process
// picks up the new user without a restart.  Falls back to restart for
// relays that don't have the API configured.
func addUUIDToRelay(cfg *config.Config, newUUID string) error {
	return withRelaySSH(cfg, func(client *gossh.Client) error {
		xrayConf, err := readRelayXrayConfig(client)
		if err != nil {
			return err
		}

		settings, clients, err := relayClients(xrayConf)
		if err != nil {
			return err
		}

		for _, c := range clients {
			if cm, ok := c.(map[string]interface{}); ok {
				if id, _ := cm["id"].(string); id == newUUID {
					return nil // already present
				}
			}
		}

		clients = append(clients, map[string]interface{}{"id": newUUID, "email": newUUID})
		settings["clients"] = clients

		if err := writeRelayXrayConfig(client, xrayConf); err != nil {
			return err
		}

		// Try the Xray API first (no restart needed).
		if err := xrayAPIAddUser(client, newUUID); err != nil {
			slog.Warn("xray API unavailable, falling back to restart", "error", err)
			restartRelayXray(client)
		}
		return nil
	})
}

// removeUUIDFromRelay connects to the relay via a temporary Xray tunnel
// and removes a client UUID from the relay's Xray config.  Like addUUIDToRelay
// it persists first, then uses the Xray API with a restart fallback.
func removeUUIDFromRelay(cfg *config.Config, targetUUID string) error {
	return withRelaySSH(cfg, func(client *gossh.Client) error {
		xrayConf, err := readRelayXrayConfig(client)
		if err != nil {
			return err
		}

		settings, clients, err := relayClients(xrayConf)
		if err != nil {
			return err
		}

		filtered := make([]interface{}, 0, len(clients))
		for _, c := range clients {
			if cm, ok := c.(map[string]interface{}); ok {
				if id, _ := cm["id"].(string); id == targetUUID {
					continue // skip — this is the one to remove
				}
			}
			filtered = append(filtered, c)
		}

		if len(filtered) == len(clients) {
			return nil // UUID not found, nothing to do
		}

		settings["clients"] = filtered

		if err := writeRelayXrayConfig(client, xrayConf); err != nil {
			return err
		}

		// Try the Xray API first (no restart needed).
		if err := xrayAPIRemoveUser(client, targetUUID); err != nil {
			slog.Warn("xray API unavailable, falling back to restart", "error", err)
			restartRelayXray(client)
		}
		return nil
	})
}
