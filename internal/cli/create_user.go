package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/config"
	twssh "github.com/tunnelwhisperer/tw/internal/ssh"
	twxray "github.com/tunnelwhisperer/tw/internal/xray"
	gossh "golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

var createUserCmd = &cobra.Command{
	Use:   "user",
	Short: "Create a client user with tunnel access",
	RunE:  runCreateUser,
}

func init() {
	createCmd.AddCommand(createUserCmd)
}

func runCreateUser(cmd *cobra.Command, args []string) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("=== Tunnel Whisperer — Create User ===")
	fmt.Println()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.Xray.RelayHost == "" {
		return fmt.Errorf("xray.relay_host must be configured before creating users")
	}
	if cfg.Xray.UUID == "" {
		return fmt.Errorf("server UUID must be set — run `tw serve` or `tw create relay-server` first")
	}

	// ── Step 1: User Name ──────────────────────────────────────────────
	fmt.Println("[1/5] User name")
	fmt.Print("      Name: ")
	scanner.Scan()
	userName := strings.TrimSpace(scanner.Text())
	if userName == "" {
		return fmt.Errorf("user name is required")
	}
	for _, r := range userName {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("user name must contain only letters, numbers, dashes, and underscores")
		}
	}

	userDir := filepath.Join(config.UsersDir(), userName)
	if _, err := os.Stat(userDir); err == nil {
		return fmt.Errorf("user %q already exists at %s", userName, userDir)
	}
	fmt.Println()

	// ── Step 2: Port Mappings ──────────────────────────────────────────
	fmt.Println("[2/5] Port mappings")
	fmt.Println("      Map client local ports to server ports (localhost only).")
	fmt.Println("      Enter mappings one at a time. Empty client port to finish.")
	fmt.Println()

	type portMapping struct {
		ClientPort int
		ServerPort int
	}
	var mappings []portMapping

	for i := 1; ; i++ {
		fmt.Printf("      Mapping %d:\n", i)
		fmt.Printf("        Client local port: ")
		scanner.Scan()
		clientPortStr := strings.TrimSpace(scanner.Text())
		if clientPortStr == "" {
			if len(mappings) == 0 {
				return fmt.Errorf("at least one port mapping is required")
			}
			break
		}
		clientPort, err := strconv.Atoi(clientPortStr)
		if err != nil || clientPort < 1 || clientPort > 65535 {
			return fmt.Errorf("invalid port: %s", clientPortStr)
		}

		fmt.Printf("        Server port:       ")
		scanner.Scan()
		serverPortStr := strings.TrimSpace(scanner.Text())
		if serverPortStr == "" {
			return fmt.Errorf("server port is required")
		}
		serverPort, err := strconv.Atoi(serverPortStr)
		if err != nil || serverPort < 1 || serverPort > 65535 {
			return fmt.Errorf("invalid port: %s", serverPortStr)
		}

		mappings = append(mappings, portMapping{ClientPort: clientPort, ServerPort: serverPort})
		fmt.Printf("        → localhost:%d (client) → 127.0.0.1:%d (server)\n", clientPort, serverPort)
		fmt.Println()
	}
	fmt.Println()

	// ── Step 3: Generate Credentials ───────────────────────────────────
	fmt.Println("[3/5] Generating credentials")

	clientUUID := uuid.New().String()
	fmt.Printf("      UUID: %s\n", clientUUID)

	privPEM, pubAuthorized, err := twssh.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generating SSH key pair: %w", err)
	}
	fmt.Println("      SSH key pair generated")
	fmt.Println()

	// ── Step 4: Update Relay ───────────────────────────────────────────
	fmt.Println("[4/5] Updating relay Xray config")
	fmt.Println("      Connecting to relay via Xray tunnel...")
	if err := addUUIDToRelay(cfg, clientUUID); err != nil {
		fmt.Printf("      Warning: relay update failed: %v\n", err)
		fmt.Println("      You may need to manually add the UUID to the relay's Xray config.")
		fmt.Printf("      UUID to add: %s\n", clientUUID)
	} else {
		fmt.Println("      OK — UUID added and Xray restarted on relay")
	}
	fmt.Println()

	// ── Step 5: Save Configuration ─────────────────────────────────────
	fmt.Println("[5/5] Saving configuration")

	if err := os.MkdirAll(userDir, 0700); err != nil {
		return fmt.Errorf("creating user directory: %w", err)
	}

	// Write client SSH keys.
	if err := os.WriteFile(filepath.Join(userDir, "id_ed25519"), privPEM, 0600); err != nil {
		return fmt.Errorf("writing client private key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "id_ed25519.pub"), pubAuthorized, 0644); err != nil {
		return fmt.Errorf("writing client public key: %w", err)
	}

	// Build client config.
	tunnels := make([]config.Tunnel, len(mappings))
	serverPorts := make([]int, len(mappings))
	for i, m := range mappings {
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
			SSHUser:       userName,
			ServerSSHPort: cfg.Server.RemotePort,
			Tunnels:       tunnels,
		},
	}

	cfgData, err := yaml.Marshal(clientCfg)
	if err != nil {
		return fmt.Errorf("marshaling client config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "config.yaml"), cfgData, 0644); err != nil {
		return fmt.Errorf("writing client config: %w", err)
	}

	// Add client's public key to server's authorized_keys with permitopen.
	// Lock down to 127.0.0.1 only — no access to the server's network.
	if err := appendAuthorizedKey(pubAuthorized, userName, serverPorts); err != nil {
		return fmt.Errorf("updating authorized_keys: %w", err)
	}

	var permits []string
	for _, port := range serverPorts {
		permits = append(permits, fmt.Sprintf("127.0.0.1:%d", port))
	}
	fmt.Printf("      Added to authorized_keys (permits: %s)\n", strings.Join(permits, ", "))
	fmt.Printf("      Client config: %s/\n", userDir)

	fmt.Println()
	fmt.Println("=== User created ===")
	fmt.Println()
	fmt.Printf("  Send the contents of %s/ to the client.\n", userDir)
	fmt.Println("  The client places these files in their config directory and runs `tw connect`.")
	fmt.Println()

	return nil
}

// appendAuthorizedKey adds a public key to the server's authorized_keys
// with permitopen restrictions for the given ports.
func appendAuthorizedKey(pubKey []byte, comment string, ports []int) error {
	akPath := config.AuthorizedKeysPath()

	// Build permitopen options.
	var options []string
	for _, port := range ports {
		options = append(options, fmt.Sprintf(`permitopen="127.0.0.1:%d"`, port))
	}

	// pubKey is in "ssh-ed25519 AAAA...\n" format from MarshalAuthorizedKey.
	keyLine := strings.TrimSpace(string(pubKey))
	line := fmt.Sprintf("%s %s %s@tw\n", strings.Join(options, ","), keyLine, comment)

	// Read existing content and append.
	existing, err := os.ReadFile(akPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading authorized_keys: %w", err)
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing = append(existing, '\n')
	}

	return os.WriteFile(akPath, append(existing, []byte(line)...), 0600)
}

// addUUIDToRelay connects to the relay via a temporary Xray tunnel and
// adds the new client UUID to the relay's Xray config.
func addUUIDToRelay(cfg *config.Config, newUUID string) error {
	// Start temporary Xray instance on a high port to avoid conflicts
	// with a running `tw serve` instance.
	xrayInstance, err := twxray.New(cfg.Xray)
	if err != nil {
		return fmt.Errorf("initializing Xray: %w", err)
	}
	const tempPort = 59000 // dokodemo-door listens on tempPort+1
	if err := xrayInstance.Start(tempPort, cfg.Server.RelaySSHPort); err != nil {
		return fmt.Errorf("starting Xray: %w", err)
	}
	defer xrayInstance.Close()

	// SSH to relay through the Xray tunnel.
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

	// Retry SSH connection — Xray needs a moment to establish the tunnel.
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

	// Read current Xray config from relay.
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	out, err := session.Output("sudo cat /usr/local/etc/xray/config.json")
	session.Close()
	if err != nil {
		return fmt.Errorf("reading relay config: %w", err)
	}

	// Parse and modify the config.
	var xrayConf map[string]interface{}
	if err := json.Unmarshal(out, &xrayConf); err != nil {
		return fmt.Errorf("parsing relay config: %w", err)
	}

	// Navigate to inbounds[0].settings.clients.
	inbounds, _ := xrayConf["inbounds"].([]interface{})
	if len(inbounds) == 0 {
		return fmt.Errorf("no inbounds in relay config")
	}
	inbound, _ := inbounds[0].(map[string]interface{})
	settings, _ := inbound["settings"].(map[string]interface{})
	clients, _ := settings["clients"].([]interface{})

	// Check if UUID already exists.
	for _, c := range clients {
		if cm, ok := c.(map[string]interface{}); ok {
			if id, _ := cm["id"].(string); id == newUUID {
				return nil // Already present.
			}
		}
	}

	// Add new client UUID.
	clients = append(clients, map[string]interface{}{"id": newUUID})
	settings["clients"] = clients

	updatedJSON, err := json.MarshalIndent(xrayConf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	// Write updated config to relay (sudo tee because shell redirect
	// runs as the SSH user, not root).
	session2, err := client.NewSession()
	if err != nil {
		return err
	}
	session2.Stdin = bytes.NewReader(updatedJSON)
	err = session2.Run("sudo tee /usr/local/etc/xray/config.json > /dev/null")
	session2.Close()
	if err != nil {
		return fmt.Errorf("writing relay config: %w", err)
	}

	// Restart Xray on relay. This will kill our own VLESS tunnel,
	// so the SSH session will die — that's expected and means it worked.
	session3, err := client.NewSession()
	if err != nil {
		return err
	}
	_ = session3.Run("sudo systemctl restart xray")
	session3.Close()

	return nil
}
