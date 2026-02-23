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
3. **Tunnel Layer:** Inside the Xray stream, OpenSSH handles port forwarding, encryption, and per-user authentication with `authorized_keys` restrictions.

**Key properties:**
- Zero inbound ports on both server and client — all connections are outbound to :443
- End-to-end SSH encryption — the relay never sees plaintext
- Per-user port lockdown via `permitopen` in `authorized_keys`

---

## Quick Start

### 1. Build

Requires **Go 1.22+**.

```bash
go build -o bin/tw ./cmd/tw
```

Cross-compile for Windows:
```bash
GOOS=windows GOARCH=amd64 go build -o bin/tw.exe ./cmd/tw
```

### 2. Provision a relay

Interactive wizard that deploys a relay VM with Caddy + Xray + firewall via Terraform:

```bash
tw create relay-server
```

Supports **Hetzner**, **DigitalOcean**, and **AWS**. The wizard walks through:
1. SSH key generation
2. Xray UUID generation
3. Relay domain configuration
4. Cloud provider selection and credentials
5. Terraform provisioning
6. DNS setup and TLS readiness check

### 3. Create a client user

On the server, create a user with locked-down port access:

```bash
tw create user
```

This generates:
- A unique Xray UUID for the client
- An SSH key pair
- A ready-to-use client config with tunnel mappings
- Updates the relay's Xray config with the new UUID (via SSH)
- Adds the client's public key to the server's `authorized_keys` with `permitopen` restrictions

The output directory (e.g., `/etc/tw/config/users/alice/`) is sent to the client.

### 4. Start the server

```bash
tw serve
```

This starts:
1. Embedded SSH server on `:2222`
2. Xray tunnel to the relay (VLESS + splitHTTP + TLS)
3. SSH reverse port forward through Xray to the relay
4. gRPC API server on `:50051`

### 5. Connect as a client

On the client machine, place the config files from step 3 into the config directory, then:

```bash
tw connect
```

This starts:
1. Xray client tunnel to the relay
2. SSH connection through Xray to the server
3. Local port listeners that forward through the SSH tunnel

---

## CLI Commands

| Command | Description |
|---------|-------------|
| `tw serve` | Start the server (SSH, Xray tunnel, reverse port forward, gRPC API) |
| `tw connect` | Connect to the server as a client (Xray tunnel, SSH port forwarding) |
| `tw create relay-server` | Interactive wizard to provision a relay VM on a cloud provider |
| `tw create user` | Create a client user with UUID, SSH keys, and port restrictions |
| `tw dashboard` | Start the web dashboard (status page) |

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
      remote_port: 5432          # PostgreSQL
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
- **Authentication:** SSH public key authentication. Each client gets a unique key pair.
- **Authorization:** `permitopen` restrictions in `authorized_keys` limit each client to specific `127.0.0.1:<port>` targets — no access to the server's wider network.
- **Dynamic keys:** The SSH server re-reads `authorized_keys` on every authentication attempt. Adding or revoking users takes effect immediately without restarting `tw serve`.
- **Relay isolation:** SSH on the relay listens on `127.0.0.1` only. The firewall exposes only ports 80 (ACME) and 443 (HTTPS). The relay never sees plaintext — it's just a transport passthrough.
- **Per-user UUIDs:** Each client has a unique Xray UUID registered on the relay, allowing individual revocation at the transport layer.

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
