package ops

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tunnelwhisperer/tw/internal/config"
	"github.com/tunnelwhisperer/tw/internal/relay/terraform"
	gossh "golang.org/x/crypto/ssh"
)

// RelayProvisionRequest contains everything needed to provision a relay.
type RelayProvisionRequest struct {
	Domain       string `json:"domain"`
	ProviderKey  string `json:"provider_key"`  // "hetzner", "digitalocean", "aws"
	ProviderName string `json:"provider_name"` // display name
	Token        string `json:"token"`
	AWSSecretKey string `json:"aws_secret_key"`
}

// RelayStatus describes the current state of the relay.
type RelayStatus struct {
	Provisioned bool   `json:"provisioned"`
	Domain      string `json:"domain"`
	IP          string `json:"ip,omitempty"`
	Provider    string `json:"provider,omitempty"`
}

// GetRelayStatus checks if a relay has been provisioned.
func (o *Ops) GetRelayStatus() RelayStatus {
	cfg := o.Config()
	relayDir := config.RelayDir()

	status := RelayStatus{
		Domain: cfg.Xray.RelayHost,
	}

	// Check for tfstate to determine if provisioned.
	if _, err := os.Stat(filepath.Join(relayDir, "terraform.tfstate")); err == nil {
		status.Provisioned = true
		// Try to read the IP from terraform output.
		ip, err := o.TerraformOutput(relayDir, nil, "relay_ip")
		if err == nil {
			status.IP = ip
		}
		// Detect provider from main.tf.
		if data, err := os.ReadFile(filepath.Join(relayDir, "main.tf")); err == nil {
			tf := string(data)
			switch {
			case strings.Contains(tf, `provider "hcloud"`):
				status.Provider = "Hetzner"
			case strings.Contains(tf, `provider "digitalocean"`):
				status.Provider = "DigitalOcean"
			case strings.Contains(tf, `provider "aws"`):
				status.Provider = "AWS"
			}
		}
	}

	return status
}

// ProvisionRelay runs the full 9-step relay provisioning flow.
// Progress events are sent through the callback. This method blocks until
// the relay is provisioned or the context is cancelled.
func (o *Ops) ProvisionRelay(ctx context.Context, req RelayProvisionRequest, progress ProgressFunc) error {
	if progress == nil {
		progress = func(ProgressEvent) {}
	}

	relayDir := config.RelayDir()

	// Step 1: SSH keys.
	progress(ProgressEvent{Step: 1, Total: 9, Label: "SSH keys", Status: "running"})
	if err := o.EnsureKeys(); err != nil {
		progress(ProgressEvent{Step: 1, Total: 9, Label: "SSH keys", Status: "failed", Error: err.Error()})
		return err
	}
	progress(ProgressEvent{Step: 1, Total: 9, Label: "SSH keys", Status: "completed"})

	// Step 2: Xray UUID.
	progress(ProgressEvent{Step: 2, Total: 9, Label: "Xray UUID", Status: "running"})
	o.mu.Lock()
	cfg := o.cfg
	if cfg.Xray.UUID == "" {
		cfg.Xray.UUID = uuid.New().String()
		if err := config.Save(cfg); err != nil {
			o.mu.Unlock()
			progress(ProgressEvent{Step: 2, Total: 9, Label: "Xray UUID", Status: "failed", Error: err.Error()})
			return fmt.Errorf("saving config: %w", err)
		}
	}
	o.mu.Unlock()
	progress(ProgressEvent{Step: 2, Total: 9, Label: "Xray UUID", Status: "completed", Message: cfg.Xray.UUID})

	// Step 3: Domain.
	progress(ProgressEvent{Step: 3, Total: 9, Label: "Relay domain", Status: "running"})
	o.mu.Lock()
	if req.Domain != "" {
		cfg.Xray.RelayHost = req.Domain
		if err := config.Save(cfg); err != nil {
			o.mu.Unlock()
			progress(ProgressEvent{Step: 3, Total: 9, Label: "Relay domain", Status: "failed", Error: err.Error()})
			return fmt.Errorf("saving config: %w", err)
		}
	}
	relayHost := cfg.Xray.RelayHost
	o.mu.Unlock()
	if relayHost == "" {
		progress(ProgressEvent{Step: 3, Total: 9, Label: "Relay domain", Status: "failed", Error: "domain is required"})
		return fmt.Errorf("relay domain is required")
	}
	progress(ProgressEvent{Step: 3, Total: 9, Label: "Relay domain", Status: "completed", Message: relayHost})

	// Step 4: Cloud provider (already selected via req).
	progress(ProgressEvent{Step: 4, Total: 9, Label: "Cloud provider", Status: "completed", Message: req.ProviderName})

	// Step 5: Credentials (already provided via req).
	progress(ProgressEvent{Step: 5, Total: 9, Label: "Credentials", Status: "running"})
	if err := o.TestCloudCredentials(req.ProviderName, req.Token, req.AWSSecretKey); err != nil {
		progress(ProgressEvent{Step: 5, Total: 9, Label: "Credentials", Status: "failed", Error: err.Error()})
		return fmt.Errorf("credential test failed: %w", err)
	}
	progress(ProgressEvent{Step: 5, Total: 9, Label: "Credentials", Status: "completed"})

	// Step 6: Not used in dashboard flow (confirmation is done by the frontend).
	progress(ProgressEvent{Step: 6, Total: 9, Label: "Confirmation", Status: "completed"})

	// Step 7: Terraform provisioning.
	progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "running", Message: "Generating Terraform files"})

	pubKeyPath := filepath.Join(config.Dir(), "id_ed25519.pub")
	pubKeyBytes, err := os.ReadFile(pubKeyPath)
	if err != nil {
		progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "failed", Error: err.Error()})
		return fmt.Errorf("reading public key: %w", err)
	}

	tfCfg := terraform.Config{
		Domain:    cfg.Xray.RelayHost,
		UUID:      cfg.Xray.UUID,
		XrayPath:  cfg.Xray.Path,
		SSHUser:   cfg.Server.RelaySSHUser,
		PublicKey: strings.TrimSpace(string(pubKeyBytes)),
		Provider:  req.ProviderKey,
	}
	if err := terraform.Generate(relayDir, tfCfg); err != nil {
		progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "failed", Error: err.Error()})
		return fmt.Errorf("generating terraform files: %w", err)
	}

	// Write credentials.
	tfEnv := map[string]string{}
	if req.ProviderName == "AWS" {
		tfEnv["AWS_ACCESS_KEY_ID"] = req.Token
		tfEnv["AWS_SECRET_ACCESS_KEY"] = req.AWSSecretKey
	} else {
		// Find the VarName for this provider.
		for _, p := range CloudProviders() {
			if p.Key == req.ProviderKey {
				tfvars := fmt.Sprintf("%s = %q\n", p.VarName, req.Token)
				tfvarsPath := filepath.Join(relayDir, "terraform.tfvars")
				if err := os.WriteFile(tfvarsPath, []byte(tfvars), 0600); err != nil {
					progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "failed", Error: err.Error()})
					return fmt.Errorf("writing terraform.tfvars: %w", err)
				}
				break
			}
		}
	}

	progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "running", Message: "terraform init"})
	if err := o.RunTerraform(ctx, relayDir, tfEnv, progress, "init"); err != nil {
		progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "failed", Error: err.Error()})
		return err
	}

	progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "running", Message: "terraform apply"})
	if err := o.RunTerraform(ctx, relayDir, tfEnv, progress, "apply", "-auto-approve"); err != nil {
		progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "failed", Error: err.Error()})
		return err
	}

	relayIP, err := o.TerraformOutput(relayDir, tfEnv, "relay_ip")
	if err != nil {
		progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "failed", Error: err.Error()})
		return fmt.Errorf("could not read relay IP: %w", err)
	}
	progress(ProgressEvent{Step: 7, Total: 9, Label: "Provisioning", Status: "completed", Message: "Relay IP: " + relayIP, Data: relayIP})

	// Step 8: DNS & readiness.
	progress(ProgressEvent{Step: 8, Total: 9, Label: "DNS & readiness", Status: "running", Message: fmt.Sprintf("Create A record: %s → %s", cfg.Xray.RelayHost, relayIP)})

	if err := o.WaitForDNS(ctx, cfg.Xray.RelayHost, relayIP, progress); err != nil {
		slog.Warn("DNS wait timed out", "error", err)
		progress(ProgressEvent{Step: 8, Total: 9, Label: "DNS & readiness", Status: "running", Message: "DNS timeout — Caddy will retry once DNS propagates"})
	}

	if err := o.WaitForRelay(ctx, cfg.Xray.RelayHost, 5*time.Minute, progress); err != nil {
		slog.Warn("relay readiness timed out", "error", err)
		progress(ProgressEvent{Step: 8, Total: 9, Label: "DNS & readiness", Status: "completed", Message: "Relay may still be initializing — try tw serve in a few minutes"})
	} else {
		progress(ProgressEvent{Step: 8, Total: 9, Label: "DNS & readiness", Status: "completed", Message: "Relay is ready"})
	}

	// Step 9: Cloud-init log (best-effort).
	progress(ProgressEvent{Step: 9, Total: 9, Label: "Cloud-init log", Status: "running", Message: "Reading cloud-init output from relay..."})
	o.ReadCloudInitLog(cfg, progress)
	progress(ProgressEvent{Step: 9, Total: 9, Label: "Cloud-init log", Status: "completed"})

	return nil
}

// DestroyRelay tears down the relay infrastructure.
func (o *Ops) DestroyRelay(ctx context.Context, creds map[string]string, progress ProgressFunc) error {
	if progress == nil {
		progress = func(ProgressEvent) {}
	}

	relayDir := config.RelayDir()
	if _, err := os.Stat(filepath.Join(relayDir, "terraform.tfstate")); os.IsNotExist(err) {
		return fmt.Errorf("no relay to destroy (no tfstate found)")
	}

	progress(ProgressEvent{Step: 1, Total: 2, Label: "Destroying relay", Status: "running"})
	if err := o.RunTerraform(ctx, relayDir, creds, progress, "destroy", "-auto-approve"); err != nil {
		progress(ProgressEvent{Step: 1, Total: 2, Label: "Destroying relay", Status: "failed", Error: err.Error()})
		return err
	}
	progress(ProgressEvent{Step: 1, Total: 2, Label: "Destroying relay", Status: "completed"})

	progress(ProgressEvent{Step: 2, Total: 2, Label: "Cleaning up", Status: "running"})
	if err := os.RemoveAll(relayDir); err != nil {
		progress(ProgressEvent{Step: 2, Total: 2, Label: "Cleaning up", Status: "failed", Error: err.Error()})
		return fmt.Errorf("removing relay directory: %w", err)
	}
	progress(ProgressEvent{Step: 2, Total: 2, Label: "Cleaning up", Status: "completed"})

	return nil
}

// TestRelay runs connectivity checks against the relay, streaming each
// result as a progress event so the dashboard shows real-time feedback.
func (o *Ops) TestRelay(progress ProgressFunc) {
	if progress == nil {
		progress = func(ProgressEvent) {}
	}

	cfg := o.Config()
	domain := cfg.Xray.RelayHost

	// 1. DNS resolution.
	progress(ProgressEvent{Step: 1, Total: 3, Label: "DNS", Status: "running"})
	addrs, err := net.LookupHost(domain)
	if err != nil {
		progress(ProgressEvent{Step: 1, Total: 3, Label: "DNS", Status: "failed", Error: err.Error()})
		return
	}
	progress(ProgressEvent{Step: 1, Total: 3, Label: "DNS", Status: "completed", Message: strings.Join(addrs, ", ")})

	// 2. HTTPS (Caddy).
	progress(ProgressEvent{Step: 2, Total: 3, Label: "HTTPS (Caddy)", Status: "running"})
	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Get("https://" + domain)
	if err != nil {
		progress(ProgressEvent{Step: 2, Total: 3, Label: "HTTPS (Caddy)", Status: "failed", Error: err.Error()})
		return
	}
	resp.Body.Close()
	progress(ProgressEvent{Step: 2, Total: 3, Label: "HTTPS (Caddy)", Status: "completed", Message: fmt.Sprintf("HTTP %d", resp.StatusCode)})

	// 3. Xray + SSH through tunnel.
	progress(ProgressEvent{Step: 3, Total: 3, Label: "Xray + SSH", Status: "running"})
	err = withRelaySSH(cfg, func(client *gossh.Client) error {
		session, err := client.NewSession()
		if err != nil {
			return err
		}
		defer session.Close()
		_, err = session.Output("echo ok")
		return err
	})
	if err != nil {
		progress(ProgressEvent{Step: 3, Total: 3, Label: "Xray + SSH", Status: "failed", Error: err.Error()})
	} else {
		progress(ProgressEvent{Step: 3, Total: 3, Label: "Xray + SSH", Status: "completed", Message: "tunnel and shell working"})
	}
}

// ReadCloudInitLog connects to the relay via the Xray tunnel and reads
// /var/log/cloud-init-output.log, streaming each line as a progress event.
// This is best-effort: errors are reported as progress messages but do not
// cause provisioning to fail.
func (o *Ops) ReadCloudInitLog(cfg *config.Config, progress ProgressFunc) {
	err := withRelaySSH(cfg, func(client *gossh.Client) error {
		session, err := client.NewSession()
		if err != nil {
			return err
		}
		defer session.Close()

		out, err := session.Output("sudo cat /var/log/cloud-init-output.log 2>/dev/null")
		if err != nil {
			return err
		}

		for _, line := range strings.Split(string(out), "\n") {
			if line != "" {
				progress(ProgressEvent{Message: line})
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("could not read cloud-init log", "error", err)
		progress(ProgressEvent{Message: fmt.Sprintf("Could not read cloud-init log: %v", err)})
	}
}

// WaitForDNS polls DNS every 3 seconds until the domain resolves to the
// expected IP. Each lookup result is streamed as a log line (Step 0) so
// the dashboard appends each attempt rather than overwriting in place.
// The loop runs indefinitely until DNS matches or the context is cancelled.
func (o *Ops) WaitForDNS(ctx context.Context, domain, expectedIP string, progress ProgressFunc) error {
	if progress == nil {
		progress = func(ProgressEvent) {}
	}

	// Initial instruction line (structured step — updates in place).
	progress(ProgressEvent{Step: 8, Total: 9, Label: "DNS", Status: "running",
		Message: fmt.Sprintf("Point A record: %s → %s", domain, expectedIP)})

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		addrs, err := net.LookupHost(domain)
		if err != nil {
			progress(ProgressEvent{Message: fmt.Sprintf("  resolve %s → %v", domain, err)})
		} else if len(addrs) == 0 {
			progress(ProgressEvent{Message: fmt.Sprintf("  resolve %s → (no records)", domain)})
		} else {
			for _, addr := range addrs {
				if addr == expectedIP {
					progress(ProgressEvent{Message: fmt.Sprintf("  resolve %s → %s ✓", domain, addr)})
					return nil
				}
			}
			progress(ProgressEvent{Message: fmt.Sprintf("  resolve %s → %s (waiting for %s)", domain, strings.Join(addrs, ", "), expectedIP)})
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// WaitForRelay polls the relay until HTTPS responds.
func (o *Ops) WaitForRelay(ctx context.Context, domain string, timeout time.Duration, progress ProgressFunc) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get("https://" + domain)
		if err == nil {
			resp.Body.Close()
			return nil
		}

		if progress != nil {
			progress(ProgressEvent{Step: 8, Total: 9, Label: "DNS & readiness", Status: "running", Message: "Waiting for HTTPS (Caddy + TLS)..."})
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
	return fmt.Errorf("timeout: no HTTPS response from %s", domain)
}
