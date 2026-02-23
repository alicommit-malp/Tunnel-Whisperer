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
)

// RelayProvisionRequest contains everything needed to provision a relay.
type RelayProvisionRequest struct {
	Domain       string
	ProviderKey  string // "hetzner", "digitalocean", "aws"
	ProviderName string // display name
	Token        string
	AWSSecretKey string
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

// ProvisionRelay runs the full 8-step relay provisioning flow.
// Progress events are sent through the callback. This method blocks until
// the relay is provisioned or the context is cancelled.
func (o *Ops) ProvisionRelay(ctx context.Context, req RelayProvisionRequest, progress ProgressFunc) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if progress == nil {
		progress = func(ProgressEvent) {}
	}

	relayDir := config.RelayDir()

	// Step 1: SSH keys.
	progress(ProgressEvent{Step: 1, Total: 8, Label: "SSH keys", Status: "running"})
	if err := o.EnsureKeys(); err != nil {
		progress(ProgressEvent{Step: 1, Total: 8, Label: "SSH keys", Status: "failed", Error: err.Error()})
		return err
	}
	progress(ProgressEvent{Step: 1, Total: 8, Label: "SSH keys", Status: "completed"})

	// Step 2: Xray UUID.
	progress(ProgressEvent{Step: 2, Total: 8, Label: "Xray UUID", Status: "running"})
	cfg := o.cfg
	if cfg.Xray.UUID == "" {
		cfg.Xray.UUID = uuid.New().String()
		if err := config.Save(cfg); err != nil {
			progress(ProgressEvent{Step: 2, Total: 8, Label: "Xray UUID", Status: "failed", Error: err.Error()})
			return fmt.Errorf("saving config: %w", err)
		}
	}
	progress(ProgressEvent{Step: 2, Total: 8, Label: "Xray UUID", Status: "completed", Message: cfg.Xray.UUID})

	// Step 3: Domain.
	progress(ProgressEvent{Step: 3, Total: 8, Label: "Relay domain", Status: "running"})
	if req.Domain != "" {
		cfg.Xray.RelayHost = req.Domain
		if err := config.Save(cfg); err != nil {
			progress(ProgressEvent{Step: 3, Total: 8, Label: "Relay domain", Status: "failed", Error: err.Error()})
			return fmt.Errorf("saving config: %w", err)
		}
	}
	if cfg.Xray.RelayHost == "" {
		progress(ProgressEvent{Step: 3, Total: 8, Label: "Relay domain", Status: "failed", Error: "domain is required"})
		return fmt.Errorf("relay domain is required")
	}
	progress(ProgressEvent{Step: 3, Total: 8, Label: "Relay domain", Status: "completed", Message: cfg.Xray.RelayHost})

	// Step 4: Cloud provider (already selected via req).
	progress(ProgressEvent{Step: 4, Total: 8, Label: "Cloud provider", Status: "completed", Message: req.ProviderName})

	// Step 5: Credentials (already provided via req).
	progress(ProgressEvent{Step: 5, Total: 8, Label: "Credentials", Status: "running"})
	if err := o.TestCloudCredentials(req.ProviderName, req.Token, req.AWSSecretKey); err != nil {
		progress(ProgressEvent{Step: 5, Total: 8, Label: "Credentials", Status: "failed", Error: err.Error()})
		return fmt.Errorf("credential test failed: %w", err)
	}
	progress(ProgressEvent{Step: 5, Total: 8, Label: "Credentials", Status: "completed"})

	// Step 6: Not used in dashboard flow (confirmation is done by the frontend).
	progress(ProgressEvent{Step: 6, Total: 8, Label: "Confirmation", Status: "completed"})

	// Step 7: Terraform provisioning.
	progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "running", Message: "Generating Terraform files"})

	pubKeyPath := filepath.Join(config.Dir(), "id_ed25519.pub")
	pubKeyBytes, err := os.ReadFile(pubKeyPath)
	if err != nil {
		progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "failed", Error: err.Error()})
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
		progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "failed", Error: err.Error()})
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
					progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "failed", Error: err.Error()})
					return fmt.Errorf("writing terraform.tfvars: %w", err)
				}
				break
			}
		}
	}

	progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "running", Message: "terraform init"})
	if err := o.RunTerraform(ctx, relayDir, tfEnv, progress, "init"); err != nil {
		progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "failed", Error: err.Error()})
		return err
	}

	progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "running", Message: "terraform apply"})
	if err := o.RunTerraform(ctx, relayDir, tfEnv, progress, "apply", "-auto-approve"); err != nil {
		progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "failed", Error: err.Error()})
		return err
	}

	relayIP, err := o.TerraformOutput(relayDir, tfEnv, "relay_ip")
	if err != nil {
		progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "failed", Error: err.Error()})
		return fmt.Errorf("could not read relay IP: %w", err)
	}
	progress(ProgressEvent{Step: 7, Total: 8, Label: "Provisioning", Status: "completed", Message: "Relay IP: " + relayIP, Data: relayIP})

	// Step 8: DNS & readiness.
	progress(ProgressEvent{Step: 8, Total: 8, Label: "DNS & readiness", Status: "running", Message: fmt.Sprintf("Create A record: %s → %s", cfg.Xray.RelayHost, relayIP)})

	if err := o.WaitForDNS(ctx, cfg.Xray.RelayHost, relayIP, 5*time.Minute, progress); err != nil {
		slog.Warn("DNS wait timed out", "error", err)
		progress(ProgressEvent{Step: 8, Total: 8, Label: "DNS & readiness", Status: "running", Message: "DNS timeout — Caddy will retry once DNS propagates"})
	}

	if err := o.WaitForRelay(ctx, cfg.Xray.RelayHost, 5*time.Minute, progress); err != nil {
		slog.Warn("relay readiness timed out", "error", err)
		progress(ProgressEvent{Step: 8, Total: 8, Label: "DNS & readiness", Status: "completed", Message: "Relay may still be initializing — try tw serve in a few minutes"})
	} else {
		progress(ProgressEvent{Step: 8, Total: 8, Label: "DNS & readiness", Status: "completed", Message: "Relay is ready"})
	}

	return nil
}

// DestroyRelay tears down the relay infrastructure.
func (o *Ops) DestroyRelay(ctx context.Context, creds map[string]string, progress ProgressFunc) error {
	o.mu.Lock()
	defer o.mu.Unlock()

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

// WaitForDNS polls DNS until the domain resolves to the expected IP.
func (o *Ops) WaitForDNS(ctx context.Context, domain, expectedIP string, timeout time.Duration, progress ProgressFunc) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		addrs, err := net.LookupHost(domain)
		if err == nil {
			for _, addr := range addrs {
				if addr == expectedIP {
					return nil
				}
			}
		}

		if progress != nil {
			progress(ProgressEvent{Step: 8, Total: 8, Label: "DNS & readiness", Status: "running", Message: fmt.Sprintf("Waiting for DNS (%s → %s)...", domain, expectedIP)})
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	return fmt.Errorf("timeout: %s did not resolve to %s", domain, expectedIP)
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
			progress(ProgressEvent{Step: 8, Total: 8, Label: "DNS & readiness", Status: "running", Message: "Waiting for HTTPS (Caddy + TLS)..."})
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
	}
	return fmt.Errorf("timeout: no HTTPS response from %s", domain)
}
