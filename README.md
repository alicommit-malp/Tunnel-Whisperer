# Tunnel Whisperer

**Surgical, resilient connectivity for restrictive enterprise environments.**

[![Status](https://img.shields.io/badge/Status-Pre--Alpha-orange)](https://github.com/yourusername/tunnel-whisperer)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

Tunnel Whisperer is a toolchain for connecting specific services across separated private networks (e.g., Hospital LAN to Cloud, or Factory Floor to Vendor Support).

Unlike traditional VPNs that connect entire machines or require complex network changes, Tunnel Whisperer creates **resilient, application-layer bridges** for specific ports. It encapsulates traffic in standard web protocols (HTTPS) to ensure connectivity even in environments with aggressive firewalls and Deep Packet Inspection (DPI).

---

## The Problem: "The Connectivity Gap"

In modern enterprise environments (Healthcare, Manufacturing, Finance), "getting things done" is often blocked by rigid network policies:

1. **Strict Egress Rules:** Firewalls often block everything except Port 443 (HTTPS). Standard tools like SSH (Port 22) or OpenVPN are dropped.
2. **Legacy Devices:** MRI scanners, industrial PLCs, and old servers often cannot install modern VPN clients (Tailscale/ZeroTier).
3. **DPI Interference:** sophisticated "Next-Gen" firewalls analyze traffic packet headers. They can detect and kill non-web traffic even if it tries to use Port 443.

**Tunnel Whisperer bridges this gap.** It wraps your traffic (SSH, TCP) inside a genuine TLS-encrypted HTTPS stream using Xray's VLESS+splitHTTP protocol. To the network, your data transfer looks exactly like standard HTTPS traffic, ensuring high reliability without policy violations.

---

## Use Cases

### 1. Healthcare Interoperability (DICOM/HL7)

**Scenario:** You need to send X-Ray images (DICOM) from a hospital scanner to a cloud-based AI analysis platform.

* **The Friction:** Hospital IT requires 6 months to approve a Site-to-Site VPN. The MRI machine is a "black box" appliance that you can't install software on.

* **The Solution:** Connect a small gateway device (e.g., Raspberry Pi) to the scanner's LAN. Tunnel Whisperer forwards the local DICOM port (104) to your cloud endpoint via outbound HTTPS.

* **Result:** Instant, secure connectivity. The scanner sends data to "localhost", and the tunnel delivers it to the cloud.

### 2. Vendor Remote Support (OT/IoT)

**Scenario:** An industrial machine (PLC) on a factory floor is malfunctioning. The vendor needs to access the control interface (Modbus or HTTP) to diagnose it.

* **The Friction:** The factory network has no inbound access. Giving the vendor full VPN access to the factory floor is a security risk.

* **The Solution:** The factory deploys a Tunnel Whisperer agent that exposes *only* the specific maintenance port of that one machine, locked down per-user.

* **Result:** The vendor gets surgical access to fix the issue without touching the rest of the network.

### 3. Developer & Data Science Workflows

**Scenario:** A data scientist needs to query a sensitive on-premise database from a Jupyter Notebook running in the cloud.

* **The Friction:** The database is behind a corporate firewall that blocks direct SQL connections.

* **The Solution:** Tunnel Whisperer maps the database port (e.g., 5432) to the cloud environment via a resilient tunnel.

* **Result:** The notebook connects to `localhost:5432` as if the database were local.

---

## Architecture

```
Client Network                   Public Cloud                    Server Network
┌─────────────┐             ┌──────────────────┐             ┌──────────────┐
│  tw connect  │── HTTPS ──▶│     Relay VM      │◀── HTTPS ──│   tw serve   │
│             │   (Xray     │                  │   (Xray     │              │
│ local ports │   VLESS +   │  Caddy :443      │   VLESS +   │ SSH server   │
│ :5432 :3389 │   splitHTTP)│  ↕ reverse proxy │   splitHTTP)│ :2222        │
│             │             │  Xray :10000     │             │              │
│  SSH ────────────────────────────────────────────────────▶ │ port fwd     │
│  (over Xray)│             │  SSH :22 (local) │             │ → services   │
└─────────────┘             │  Firewall: 80+443│             └──────────────┘
                            └──────────────────┘
```

1. **Transport Layer:** Traffic is encapsulated in **Xray VLESS over splitHTTP + TLS** on port 443. This looks like standard HTTPS to firewalls, proxies, and DPI systems.
2. **Relay:** A lightweight cloud VM (Hetzner, DigitalOcean, or AWS) provisioned via `tw create relay-server`. It runs Caddy (TLS/ACME) and Xray (VLESS inbound). SSH listens on localhost only — no port 22 exposed.
3. **Tunnel Layer:** Inside the Xray stream, an embedded SSH server (Go `x/crypto/ssh`) handles port forwarding, encryption, and per-user authentication with `authorized_keys` restrictions.

**Key properties:**
- Zero inbound ports on both server and client — all connections are outbound to :443
- End-to-end SSH encryption — the relay never sees plaintext
- Per-user port lockdown via `permitopen` in `authorized_keys`

---

## Quick Start

### 1. Build

Requires **Go 1.22+** and **Terraform** (for relay provisioning).

```bash
go build -o bin/tw ./cmd/tw
```

Cross-compile for Windows:
```bash
GOOS=windows GOARCH=amd64 go build -o bin/tw.exe ./cmd/tw
```

### 2. Provision a relay

Interactive 8-step wizard that deploys a relay VM with Caddy + Xray + firewall via Terraform:

```bash
tw create relay-server
```

Supports **Hetzner**, **DigitalOcean**, and **AWS**. The wizard walks through:

1. **SSH key generation** — creates ed25519 key pair if missing
2. **Xray UUID generation** — creates or reuses the server's transport UUID
3. **Relay domain** — sets `xray.relay_host` (e.g. `relay.example.com`)
4. **Cloud provider selection** — choose Hetzner, DigitalOcean, or AWS
5. **Credentials** — enter API token (Hetzner/DO) or Access Key + Secret (AWS); credentials are validated against the provider API before proceeding
6. **Credential test** — Hetzner/DO tokens are tested via API call; AWS keys are format-checked (full validation happens during `terraform apply`)
7. **Terraform provisioning** — generates cloud-init + provider-specific `main.tf`, runs `terraform init` and `terraform apply`, outputs the relay's public IP
8. **DNS + HTTPS readiness** — prompts you to create an A record, then polls DNS resolution and waits for Caddy to issue the TLS certificate

If a relay already exists (terraform state present), the wizard offers to destroy and recreate it.

The relay VM is configured via cloud-init to:
- Create an SSH user with the server's public key and passwordless sudo
- Install Caddy (from official apt repo) and Xray (from official install script)
- Write Xray config: VLESS inbound on `127.0.0.1:10000` with splitHTTP transport
- Write Caddyfile: reverse proxy `<domain>` path `/tw*` to Xray
- Lock SSH to `127.0.0.1` only with password auth disabled
- Configure firewall (ufw): deny all incoming, allow 80/tcp + 443/tcp

### 3. Create a client user

On the server, create a user with locked-down port access:

```bash
tw create user
```

Interactive 5-step wizard:

1. **User name** — alphanumeric, dashes, underscores; must be unique
2. **Port mappings** — define one or more mappings sequentially:
   - Client local port (the port the client listens on)
   - Server port (the `127.0.0.1` port on the server to forward to)
   - Remote host is locked to `127.0.0.1` — no access to the server's wider network
3. **Generate credentials** — creates a unique Xray UUID and ed25519 SSH key pair for the client
4. **Update relay** — connects to the relay via a temporary Xray tunnel (port 59001), SSHs in, adds the new UUID to the relay's Xray config (`/usr/local/etc/xray/config.json`), and restarts Xray on the relay
5. **Save configuration** — writes the client's config files and keys to `<config_dir>/users/<name>/`, and appends the client's public key to the server's `authorized_keys` with `permitopen` restrictions

The generated `authorized_keys` entry:
```
permitopen="127.0.0.1:5432",permitopen="127.0.0.1:8080" ssh-ed25519 AAAA... alice@tw
```

Output directory (e.g. `/etc/tw/config/users/alice/`) contains `config.yaml`, `id_ed25519`, and `id_ed25519.pub` — send these to the client.

### 4. Start the server

```bash
tw serve
```

This starts:
1. Embedded SSH server on `:2222` (Go `x/crypto/ssh`) with dynamic `authorized_keys` and `permitopen` enforcement
2. Xray in-process tunnel to the relay (VLESS + splitHTTP + TLS on port 443)
3. SSH reverse port forward through Xray to the relay (`-R 2222:localhost:2222`)
4. gRPC API server on `:50051`

### 5. Connect as a client

On the client machine, place the config files from step 3 into the config directory, then:

```bash
tw connect
```

This starts:
1. Xray client tunnel to the relay (dokodemo-door on `:54001` forwarding to the server's SSH port on the relay)
2. SSH connection through Xray to the server's embedded SSH (public key auth)
3. Local port listeners for all configured tunnel mappings, forwarding through a single SSH session

---

## CLI Commands

| Command | Description |
|---------|-------------|
| `tw serve` | Start the server (SSH server, Xray tunnel, reverse port forward, gRPC API) |
| `tw connect` | Connect to the server as a client (Xray tunnel, SSH port forwarding) |
| `tw create relay-server` | Interactive 8-step wizard to provision a relay VM on Hetzner, DigitalOcean, or AWS |
| `tw create user` | Interactive 5-step wizard to create a client user with UUID, SSH keys, and port restrictions |
| `tw dashboard` | Start the web dashboard (HTTP status page) |

---

## Configuration

Default `config.yaml`:

```yaml
xray:
  uuid: ""                       # auto-generated on first run
  relay_host: ""                 # e.g. relay.example.com
  relay_port: 443
  path: /tw                      # Xray splitHTTP path

server:                          # only used by `tw serve`
  ssh_port: 2222                 # embedded SSH server port
  api_port: 50051                # gRPC API port
  dashboard_port: 8080
  relay_ssh_port: 22             # SSH port on relay (localhost only)
  relay_ssh_user: ubuntu         # SSH user on relay
  remote_port: 2222              # port exposed on relay for clients

client:                          # only used by `tw connect`
  ssh_user: tunnel               # SSH username for server auth
  server_ssh_port: 2222          # server's SSH port on relay
  tunnels:                       # port forwarding mappings
    - local_port: 5432           # listen on client localhost
      remote_host: 127.0.0.1    # target on server (localhost only)
      remote_port: 5432          # e.g. PostgreSQL
```

Config paths:

| Platform | Path |
|----------|------|
| Linux | `/etc/tw/config/config.yaml` |
| Windows | `C:\ProgramData\tw\config\config.yaml` |

Override with `TW_CONFIG_DIR` environment variable.

---

## Security Model

- **Transport:** Xray VLESS + splitHTTP over TLS on port 443. Indistinguishable from regular HTTPS traffic.
- **Authentication:** SSH public key authentication. Each client gets a unique ed25519 key pair.
- **Authorization:** `permitopen` restrictions in `authorized_keys` limit each client to specific `127.0.0.1:<port>` targets — no access to the server's wider network.
- **Dynamic keys:** The embedded SSH server re-reads `authorized_keys` on every authentication attempt. Adding or revoking users takes effect immediately without restarting `tw serve`.
- **Relay isolation:** SSH on the relay listens on `127.0.0.1` only. The firewall exposes only ports 80 (ACME) and 443 (HTTPS). The relay never sees plaintext — it's just a transport passthrough.
- **Per-user UUIDs:** Each client has a unique Xray UUID registered on the relay, allowing individual revocation at the transport layer.

### Encryption Details

#### Transport Encryption

- All traffic is encrypted with **TLS 1.3** (HTTPS) — the same standard used by online banking and healthcare portals.
- TLS certificates are automatically provisioned and renewed via **Let's Encrypt** (ACME protocol).
- Traffic is served over standard **HTTPS (port 443)** — indistinguishable from normal web traffic to firewalls and network inspectors.

#### Tunnel Protocol

- Uses the **VLESS protocol** over **SplitHTTP transport** — traffic is split across multiple standard HTTP connections, making it resilient in restrictive network environments.
- Each user authenticates with a unique **UUID token** — no shared credentials.
- The relay server cannot read application-layer data; it only forwards encrypted streams.

#### Authentication & Key Management

- All SSH connections use **Ed25519 public key authentication** — no passwords, no brute-force attack surface.
- Each user receives an individual **Ed25519 key pair** (256-bit elliptic curve).
- SSH port forwarding is restricted per user via `permitopen` directives — users can only reach the specific services they are authorized for.

#### Network Hardening

- The relay VM firewall (UFW) allows **only ports 80 and 443** — no SSH exposed to the internet.
- SSH on the relay listens on **localhost only** (127.0.0.1) — accessible only through the encrypted tunnel.
- All management operations (user provisioning, configuration changes) happen through the encrypted tunnel, never over unprotected channels.

#### Encryption Layers Summary

| Layer              | Standard                      | Purpose                                                      |
| ------------------ | ----------------------------- | ------------------------------------------------------------ |
| TLS 1.3            | Industry standard             | Encrypts all data in transit                                 |
| VLESS + SplitHTTP  | Tunnel protocol               | Authenticates users, obfuscates traffic patterns             |
| Ed25519 SSH        | Elliptic curve cryptography   | Authenticates tunnel endpoints, restricts per-user access    |

#### Compliance-Relevant Properties

- Zero plaintext data leaves the local network — all data is encrypted before it reaches the public internet.
- No credentials or keys are stored on the relay — compromise of the relay does not expose user data.
- Supports the principle of least privilege: each user can only forward to explicitly allowed ports and services.

---

## Market Comparison

| Feature | **Tunnel Whisperer** | **Standard VPNs** (Tailscale/WireGuard) | **Reverse Proxies** (Ngrok) |
| :--- | :--- | :--- | :--- |
| **Connectivity** | **Surgical** (Port-to-Port) | **Broad** (Host-to-Host) | **Public** (Port-to-Web) |
| **Network Compatibility** | **High** (DPI-Resistant HTTPS) | **Low** (UDP/standard ports often blocked) | **Medium** (Standard HTTPS) |
| **Deployment Target** | **Gateway / Sidecar** (Connects *other* devices) | **Host-Based** (Connects *this* device) | **Dev/Test** (Temporary exposure) |
| **Infrastructure** | **Self-Hosted** (You own data/keys) | **SaaS / Hybrid** | **SaaS** |
| **Primary Goal** | **Production Reliability** in strict networks | **Mesh Networking** | **Public Access** |

---
