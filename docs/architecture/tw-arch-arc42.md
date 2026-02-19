# Tunnel Whisperer — Architecture

> Based on the [arc42](https://arc42.org) template.

---

## 1. Introduction and Goals

Tunnel Whisperer creates resilient, application-layer bridges for specific ports across separated private networks. It encapsulates traffic in standard HTTPS/WebSocket to traverse strict firewalls, NAT, and DPI-controlled environments.

### 1.1 Requirements Overview

The system connects a **server** behind a private network to **clients** behind other private networks, via a publicly reachable **relay**. All connectivity is egress-only from both sides. The relay is fully provisioned and managed by the server — no manual infrastructure setup is required.

### 1.2 Quality Goals

| Priority | Goal | Description |
| -------- | ---- | ----------- |
| 1 | Firewall traversal | Only port 443 (HTTPS) is exposed; compatible with strict corporate firewalls and DPI |
| 2 | Zero inbound ports | Neither client nor server requires any inbound connectivity |
| 3 | Transport resilience | Xray provides robust tunneling over TLS/WebSocket, surviving network disruptions |
| 4 | Session security | SSH handles authentication, encryption, and port-level access semantics |
| 5 | Automated provisioning | The relay is fully deployed, configured, and updated by the server |

---

## 3. System Scope and Context

### 3.1 Business Context

```mermaid
graph LR
    subgraph Server Network
        S[Server - tw]
    end

    subgraph Public Cloud
        R[Relay]
        C_[Caddy]
        X[Xray]
    end

    subgraph Client Network
        CL[Client - tw]
    end

    S -- "TLS :443 (Xray outbound)" --> R
    CL -- "TLS :443 (Xray outbound)" --> R
    R --- C_
    R --- X
```

### 3.2 Technical Context

| Protocol | Port | Direction | Purpose |
| -------- | ---- | --------- | ------- |
| TLS (Xray VLESS+WS) | 443 | Server → Relay | Transport tunnel for SSH reverse forwarding |
| TLS (Xray VLESS+WS) | 443 | Client → Relay | Transport tunnel for SSH local forwarding |
| HTTPS (Caddy) | 443 | External → Relay | TLS termination, OAuth endpoint, reverse proxy |
| HTTP | 80 | External → Relay | ACME challenge for certificate issuance |
| SSH | 22 | Server → Relay | Initial relay provisioning and config management |
| SSH (over Xray) | — | End-to-end | Reverse port forwarding and session security |
| SSH (embedded) | 2222 | Local | Server's embedded SSH server (Go `x/crypto/ssh`), reverse-forwarded to relay |
| gRPC | 50051 | Local | Server API for dashboard and tooling |

---

## 4. Solution Strategy

| Challenge | Solution | Technology |
| --------- | -------- | ---------- |
| Firewalls block non-HTTPS traffic | Encapsulate all traffic in TLS on port 443 | Xray (VLESS + WebSocket) |
| Server and client are behind NAT | All connections are outbound-only; relay is the rendezvous point | SSH reverse port forwarding |
| Relay must never see plaintext | End-to-end encryption between client and server | SSH session layer |
| TLS certificates for the relay | Automatic issuance and renewal | Caddy (ACME / Let's Encrypt) |
| Client authentication | OAuth + JWT before tunnel access is granted | Caddy (OAuth proxy) |
| Infrastructure provisioning | Server automates relay deployment via cloud APIs | AWS SDK (EC2, Route53, SG) |
| Cross-platform operation | Single binary for both server and client | Go (Linux + Windows) |

---

## 5. Building Block View

### 5.1 Level 1 — System Overview

#### Server

`tw` is a Go binary released for both Windows and Linux. The server brings up three internal services:

* **Core Service** — runs all commands (relay provisioning, key generation, tunnel management)
* **API Service** — a gRPC service that exposes core service operations
* **SSH Server** — an embedded SSH server (Go `golang.org/x/crypto/ssh`) that listens on a configurable port and is reverse-forwarded to the relay for client access

All settings are stored in a YAML configuration file:

| Platform | Config path |
| -------- | ----------- |
| Linux | `/etc/tw/config/config.yaml` |
| Windows | `C:\ProgramData\tw\config\config.yaml` |

Override with the `TW_CONFIG_DIR` environment variable.

#### Relay

The relay is a lightweight cloud instance provisioned and managed entirely by the server. It runs:

* **Caddy** — reverse proxy, automatic TLS certificate issuance for the subdomain, and OAuth authentication
* **Xray** — transport layer for tunneling traffic over TLS/WebSocket

The server controls the relay's configuration at any time via SSH (using a key pair generated during provisioning).

> For the initial release, only **AWS** is supported as the relay provider. More providers will follow in future releases.

#### Client

The client is the same `tw` binary connecting to the relay to reach services exposed by the server.

### 5.2 Level 2 — Project Structure

```text
tw/
├── cmd/
│   └── tw/                      # binary entry point
├── internal/
│   ├── cli/                     # cobra commands (serve, connect, dashboard)
│   ├── config/                  # YAML config loading, platform-specific paths
│   ├── core/                    # core service — orchestrates all operations
│   ├── api/                     # gRPC API service
│   ├── provider/                # cloud provider interface + implementations
│   │   └── aws/                 # AWS provider (EC2, Route53, SG)
│   ├── relay/                   # relay config generation
│   │   ├── caddy/               # Caddyfile templating
│   │   └── xray/                # Xray config templating
│   ├── ssh/                     # SSH key generation, embedded server & client
│   ├── auth/                    # OAuth provider & JWT validation
│   ├── tunnel/                  # tunnel lifecycle (server & client side)
│   └── dashboard/               # HTTP server serving embedded web UI
├── proto/                       # gRPC protobuf definitions
│   └── api/v1/
├── deploy/                      # relay config templates (go:embed)
│   ├── caddy/
│   └── xray/
├── web/                         # frontend SPA source
├── go.mod
├── go.sum
└── Makefile
```

---

## 6. Runtime View

### 6.1 Relay Provisioning

Everything starts from the server.

```mermaid
sequenceDiagram
    participant S as Server
    participant AWS as AWS APIs
    participant R as Relay

    S ->> AWS: Provision tiny EC2 instance
    S ->> AWS: Configure subdomain DNS → Relay IP
    S ->> AWS: Set firewall rules (443, 80, 22)
    S ->> S: Generate SSH key pair
    S ->> R: Copy public key (initial access)
    S ->> R: Install & configure Caddy + Xray
    R ->> R: Caddy issues TLS cert for subdomain
```

### 6.2 Phase 1 — Server ↔ Relay Setup

1. A **UUID** is generated for the server and set in the Xray config on the relay, so the server can connect.
2. The server uses **Xray + SSH** to reach the relay and performs **reverse port forwarding** (`ssh -R`) of its **SSH port** and **OAuth port**.
3. On the relay, Caddy is configured with two reverse proxies:
   * **Public endpoint** → forwards to the server's OAuth service (authentication provider)
   * **Protected endpoint** → requires a valid JWT (authenticated by the OAuth endpoint above)

```mermaid
sequenceDiagram
    participant S as Server
    participant R as Relay (Caddy + Xray)

    S ->> S: Generate UUID
    S ->> R: Set UUID in Xray config

    S ->> R: Connect via Xray (TLS :443)
    S ->> R: SSH reverse forward (SSH port + OAuth port)

    Note over R: Caddy now has:
    Note over R: 1. Public route → Server OAuth
    Note over R: 2. Protected route → Server SSH (JWT required)
```

### 6.3 Phase 2 — Client Connection

1. The client attempts to connect to the relay via Xray.
2. Caddy intercepts and requires authentication first.
3. The client authenticates against the **public OAuth endpoint** (username/password or client ID/secret) and receives a **JWT**.
4. The client connects to the **protected Caddy endpoint**, presenting the JWT.
5. Caddy validates the JWT against the OAuth provider and, if valid, forwards traffic to the server's SSH port.

```mermaid
sequenceDiagram
    participant CL as Client
    participant R as Relay (Caddy)
    participant S as Server (via reverse forward)

    CL ->> R: Request JWT (public OAuth endpoint)
    Note over CL: username/password or client_id/secret
    R -->> CL: JWT token

    CL ->> R: Connect to protected endpoint (with JWT)
    R ->> R: Validate JWT against OAuth
    R ->> S: Forward traffic to Server SSH
    CL <<-->> S: Tunnel established
```

---

## 7. Deployment View

### 7.1 Configuration

Default `config.yaml`:

```yaml
ssh:
  port: 2222
  host_key_dir: /etc/tw/config   # or C:\ProgramData\tw\config on Windows
api:
  port: 50051
dashboard:
  port: 8080
relay:
  provider: aws
  domain: ""
  region: ""
xray:
  enabled: false
  uuid: ""                       # auto-generated on first run
  relay_host: ""                 # e.g. shadow.mint-tunnel.com
  relay_port: 443
  path: /tw
  relay_ssh_port: 22
  relay_ssh_user: ubuntu
  remote_port: 2222              # port exposed on relay for clients
```

### 7.2 Linux

```bash
# install as a systemd service, enable, and start it
tw serve

# bring up the dashboard UI (calls the API service)
tw dashboard
```

### 7.3 Windows

```powershell
# install as a Windows service, set to automatic, and start it
tw.exe serve

# bring up the dashboard UI (calls the API service)
tw dashboard
```

### 7.4 What `tw serve` Starts

1. Loads (or creates) `config.yaml` from the platform config directory
2. Generates an ed25519 SSH key pair (`id_ed25519` / `id_ed25519.pub`) if missing
3. Generates an ed25519 host key (`ssh_host_ed25519_key`) for the embedded SSH server
4. Starts the **embedded SSH server** on the configured port (default `:2222`)
5. If `xray.enabled`:
   * Starts **Xray** in-process (dokodemo-door → VLESS/splithttp/TLS to relay)
   * Opens an **SSH reverse tunnel** through Xray to the relay (`-R remote_port:localhost:ssh_port`)
6. Starts the **gRPC API server** on the configured port (default `:50051`)
