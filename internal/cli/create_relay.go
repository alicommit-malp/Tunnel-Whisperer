package cli

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/relay/terraform"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create infrastructure resources",
}

var createRelayServerCmd = &cobra.Command{
	Use:   "relay-server",
	Short: "Interactively provision a relay server on a cloud provider",
	RunE:  runCreateRelayServer,
}

func init() {
	createCmd.AddCommand(createRelayServerCmd)
	rootCmd.AddCommand(createCmd)
}

type cloudProvider struct {
	Name      string
	Key       string // matches terraform.Config.Provider
	TokenName string
	TokenLink string
	VarName   string // Terraform variable name for the token
}

var cloudProviders = []cloudProvider{
	{
		Name:      "Hetzner",
		Key:       "hetzner",
		TokenName: "API Token",
		TokenLink: "https://console.hetzner.cloud → Project → Security → API Tokens → Generate",
		VarName:   "hcloud_token",
	},
	{
		Name:      "DigitalOcean",
		Key:       "digitalocean",
		TokenName: "API Token",
		TokenLink: "https://cloud.digitalocean.com/account/api/tokens → Generate New Token",
		VarName:   "do_token",
	},
	{
		Name:      "AWS",
		Key:       "aws",
		TokenName: "Access Key",
		TokenLink: "https://console.aws.amazon.com/iam/ → Users → Security Credentials → Create Access Key",
	},
}

func runCreateRelayServer(cmd *cobra.Command, args []string) error {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("=== Tunnel Whisperer — Relay Server Setup ===")
	fmt.Println()

	// Pre-check: terraform must be installed.
	if _, err := exec.LookPath("terraform"); err != nil {
		return fmt.Errorf("terraform is required but not found in PATH\n  Install: https://developer.hashicorp.com/terraform/install")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Check if relay was already provisioned.
	relayDir := config.RelayDir()
	if _, err := os.Stat(filepath.Join(relayDir, "terraform.tfstate")); err == nil {
		fmt.Printf("  Relay already provisioned (state exists in %s).\n", relayDir)
		fmt.Print("  Destroy and recreate? [y/N]: ")
		scanner.Scan()
		if answer := strings.TrimSpace(strings.ToLower(scanner.Text())); answer != "y" {
			fmt.Println("  Aborted.")
			return nil
		}
		// Destroy existing resources — may need credentials.
		destroyEnv := destroyCredentials(relayDir, scanner)
		fmt.Println("  Destroying existing relay resources...")
		if err := runTerraformCmd(relayDir, destroyEnv, "destroy", "-auto-approve"); err != nil {
			fmt.Printf("  Warning: terraform destroy failed: %v\n", err)
			fmt.Println("  You may need to delete cloud resources manually.")
		}
		if err := os.RemoveAll(relayDir); err != nil {
			return fmt.Errorf("removing relay directory: %w", err)
		}
	}

	// ── Step 1: SSH Keys ────────────────────────────────────────────────
	fmt.Println("[1/8] SSH keys")
	if err := ensureKeys(cfg); err != nil {
		return err
	}
	fmt.Println("      OK")
	fmt.Println()

	// ── Step 2: Xray UUID ───────────────────────────────────────────────
	fmt.Println("[2/8] Xray UUID")
	if cfg.Xray.UUID == "" {
		cfg.Xray.UUID = uuid.New().String()
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Printf("      Generated: %s\n", cfg.Xray.UUID)
	} else {
		fmt.Printf("      Existing:  %s\n", cfg.Xray.UUID)
	}
	fmt.Println()

	// ── Step 3: Relay Domain ────────────────────────────────────────────
	fmt.Println("[3/8] Relay domain")
	if cfg.Xray.RelayHost != "" {
		fmt.Printf("      Current: %s\n", cfg.Xray.RelayHost)
		fmt.Print("      Keep? [Y/n]: ")
		scanner.Scan()
		if answer := strings.TrimSpace(strings.ToLower(scanner.Text())); answer == "n" {
			cfg.Xray.RelayHost = ""
		}
	}
	if cfg.Xray.RelayHost == "" {
		fmt.Print("      Enter relay domain (e.g. relay.example.com): ")
		scanner.Scan()
		domain := strings.TrimSpace(scanner.Text())
		if domain == "" {
			return fmt.Errorf("relay domain is required")
		}
		cfg.Xray.RelayHost = domain
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}
	fmt.Printf("      Domain: %s\n", cfg.Xray.RelayHost)
	fmt.Println()

	// ── Step 4: Cloud Provider ──────────────────────────────────────────
	fmt.Println("[4/8] Cloud provider")
	for i, p := range cloudProviders {
		fmt.Printf("      %d) %s\n", i+1, p.Name)
	}
	fmt.Print("      Select [1-3]: ")
	scanner.Scan()
	providerIdx := strings.TrimSpace(scanner.Text())
	var selected cloudProvider
	switch providerIdx {
	case "1":
		selected = cloudProviders[0]
	case "2":
		selected = cloudProviders[1]
	case "3":
		selected = cloudProviders[2]
	default:
		return fmt.Errorf("invalid choice: %s", providerIdx)
	}
	fmt.Printf("      Provider: %s\n", selected.Name)
	fmt.Println()

	// ── Step 5: Cloud Credentials ───────────────────────────────────────
	fmt.Printf("[5/8] %s credentials\n", selected.Name)
	fmt.Printf("      Generate here: %s\n", selected.TokenLink)
	fmt.Println()

	var token, awsSecretKey string
	if selected.Name == "AWS" {
		fmt.Print("      AWS Access Key ID: ")
		scanner.Scan()
		token = strings.TrimSpace(scanner.Text())
		fmt.Print("      AWS Secret Access Key: ")
		scanner.Scan()
		awsSecretKey = strings.TrimSpace(scanner.Text())
		if token == "" || awsSecretKey == "" {
			return fmt.Errorf("both AWS Access Key ID and Secret Access Key are required")
		}
	} else {
		fmt.Printf("      %s: ", selected.TokenName)
		scanner.Scan()
		token = strings.TrimSpace(scanner.Text())
		if token == "" {
			return fmt.Errorf("%s is required", selected.TokenName)
		}
	}
	fmt.Println()

	// ── Step 6: Test Credentials ────────────────────────────────────────
	fmt.Printf("[6/8] Testing %s credentials...\n", selected.Name)
	if err := testCloudToken(selected.Name, token, awsSecretKey); err != nil {
		return fmt.Errorf("credential test failed: %w", err)
	}
	fmt.Println("      OK")
	fmt.Println()

	// ── Step 7: Confirm & Create ────────────────────────────────────────
	fmt.Println("[7/8] Provisioning relay")
	fmt.Printf("      Provider:  %s\n", selected.Name)
	fmt.Printf("      Domain:    %s\n", cfg.Xray.RelayHost)
	fmt.Printf("      Instance:  Ubuntu 24.04 (smallest tier)\n")
	fmt.Printf("      Firewall:  ports 80, 443 only\n")
	fmt.Printf("      Software:  Caddy + Xray + SSH (localhost-only)\n")
	fmt.Println()
	fmt.Print("      Proceed? [Y/n]: ")
	scanner.Scan()
	if answer := strings.TrimSpace(strings.ToLower(scanner.Text())); answer == "n" {
		fmt.Println("      Aborted.")
		return nil
	}
	fmt.Println()

	// Generate Terraform + cloud-init files.
	pubKeyPath := filepath.Join(config.Dir(), "id_ed25519.pub")
	pubKeyBytes, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("reading public key: %w", err)
	}

	tfCfg := terraform.Config{
		Domain:    cfg.Xray.RelayHost,
		UUID:      cfg.Xray.UUID,
		XrayPath:  cfg.Xray.Path,
		SSHUser:   cfg.Server.RelaySSHUser,
		PublicKey: strings.TrimSpace(string(pubKeyBytes)),
		Provider:  selected.Key,
	}
	if err := terraform.Generate(relayDir, tfCfg); err != nil {
		return fmt.Errorf("generating terraform files: %w", err)
	}

	// Write credentials.
	tfEnv := map[string]string{}
	if selected.Name == "AWS" {
		tfEnv["AWS_ACCESS_KEY_ID"] = token
		tfEnv["AWS_SECRET_ACCESS_KEY"] = awsSecretKey
	} else {
		tfvars := fmt.Sprintf("%s = %q\n", selected.VarName, token)
		tfvarsPath := filepath.Join(relayDir, "terraform.tfvars")
		if err := os.WriteFile(tfvarsPath, []byte(tfvars), 0600); err != nil {
			return fmt.Errorf("writing terraform.tfvars: %w", err)
		}
	}

	// terraform init
	fmt.Println("      terraform init...")
	if err := runTerraformCmd(relayDir, tfEnv, "init"); err != nil {
		return fmt.Errorf("terraform init failed: %w", err)
	}

	// terraform apply
	fmt.Println("      terraform apply...")
	if err := runTerraformCmd(relayDir, tfEnv, "apply", "-auto-approve"); err != nil {
		return fmt.Errorf("terraform apply failed: %w", err)
	}

	// Get relay IP.
	relayIP, err := terraformOutput(relayDir, tfEnv, "relay_ip")
	if err != nil {
		return fmt.Errorf("could not read relay IP from terraform output: %w", err)
	}
	fmt.Printf("      Relay IP: %s\n", relayIP)
	fmt.Println()

	// ── Step 8: DNS & Readiness ─────────────────────────────────────────
	fmt.Println("[8/8] DNS setup")
	fmt.Printf("      Create an A record:  %s → %s\n", cfg.Xray.RelayHost, relayIP)
	fmt.Println()
	fmt.Print("      Press Enter when DNS is configured...")
	scanner.Scan()
	fmt.Println()

	// Wait for DNS.
	fmt.Printf("      Waiting for %s to resolve to %s...\n", cfg.Xray.RelayHost, relayIP)
	if err := waitForDNS(cfg.Xray.RelayHost, relayIP, 5*time.Minute); err != nil {
		fmt.Printf("      Warning: %v\n", err)
		fmt.Println("      Caddy will retry TLS acquisition once DNS propagates.")
	} else {
		fmt.Println("      DNS resolved")
	}
	fmt.Println()

	// Wait for HTTPS (Caddy + TLS ready).
	fmt.Println("      Waiting for relay HTTPS (cloud-init + Caddy TLS)...")
	if err := waitForRelay(cfg.Xray.RelayHost, 5*time.Minute); err != nil {
		fmt.Printf("      Warning: %v\n", err)
		fmt.Println("      Cloud-init may still be running. Try `tw serve` in a few minutes.")
	} else {
		fmt.Println("      Relay is ready")
	}

	fmt.Println()
	fmt.Println("=== Relay server setup complete ===")
	fmt.Println()
	fmt.Println("  Run `tw serve` to start the tunnel.")
	fmt.Println()

	return nil
}

// ── Credential Tests ────────────────────────────────────────────────────────

func testCloudToken(provider, token, awsSecret string) error {
	switch provider {
	case "Hetzner":
		return testHTTPToken("https://api.hetzner.cloud/v1/servers", token)
	case "DigitalOcean":
		return testHTTPToken("https://api.digitalocean.com/v2/account", token)
	case "AWS":
		// AWS auth requires Signature V4 — do a basic format check here,
		// full validation happens during terraform apply.
		if len(token) < 16 {
			return fmt.Errorf("Access Key ID looks too short")
		}
		if len(awsSecret) < 30 {
			return fmt.Errorf("Secret Access Key looks too short")
		}
		return nil
	}
	return nil
}

func testHTTPToken(url, token string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return fmt.Errorf("invalid token (HTTP 401)")
	}
	// 403 means the token authenticated but may lack a scope for this
	// endpoint — that's fine, Terraform only needs resource-level access.
	return nil
}

// destroyCredentials detects the provider from the existing main.tf and
// returns env vars needed for terraform destroy. For Hetzner/DO the token
// lives in terraform.tfvars (already on disk). For AWS we must ask.
func destroyCredentials(relayDir string, scanner *bufio.Scanner) map[string]string {
	mainTf, err := os.ReadFile(filepath.Join(relayDir, "main.tf"))
	if err != nil {
		return nil
	}
	// AWS provider needs credentials via env vars.
	if strings.Contains(string(mainTf), `provider "aws"`) {
		fmt.Println("  AWS credentials needed to destroy resources.")
		fmt.Print("  AWS Access Key ID: ")
		scanner.Scan()
		keyID := strings.TrimSpace(scanner.Text())
		fmt.Print("  AWS Secret Access Key: ")
		scanner.Scan()
		secret := strings.TrimSpace(scanner.Text())
		if keyID != "" && secret != "" {
			return map[string]string{
				"AWS_ACCESS_KEY_ID":     keyID,
				"AWS_SECRET_ACCESS_KEY": secret,
			}
		}
	}
	// Hetzner/DO: terraform.tfvars has the token, no extra env needed.
	return nil
}

// ── Terraform Helpers ───────────────────────────────────────────────────────

func runTerraformCmd(dir string, env map[string]string, args ...string) error {
	cmd := exec.Command("terraform", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	return cmd.Run()
}

func terraformOutput(dir string, env map[string]string, name string) (string, error) {
	cmd := exec.Command("terraform", "output", "-raw", name)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ── Readiness Checks ────────────────────────────────────────────────────────

func waitForDNS(domain, expectedIP string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addrs, err := net.LookupHost(domain)
		if err == nil {
			for _, addr := range addrs {
				if addr == expectedIP {
					return nil
				}
			}
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("timeout: %s did not resolve to %s", domain, expectedIP)
}

func waitForRelay(domain string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get("https://" + domain)
		if err == nil {
			resp.Body.Close()
			return nil // Any response from Caddy means TLS is working.
		}
		time.Sleep(15 * time.Second)
	}
	return fmt.Errorf("timeout: no HTTPS response from %s", domain)
}
