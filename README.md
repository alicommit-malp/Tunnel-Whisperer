# Tunnel Whisperer

**Surgical, resilient connectivity for restrictive enterprise environments.**

[![Status](https://img.shields.io/badge/Status-Pre--Alpha-orange)](https://github.com/yourusername/tunnel-whisperer)
[![License](https://img.shields.io/badge/License-MIT-blue)](LICENSE)

Tunnel Whisperer is a toolchain for connecting specific services across separated private networks (e.g., Hospital LAN to Cloud, or Factory Floor to Vendor Support).

Unlike traditional VPNs that connect entire machines or require complex network changes, Tunnel Whisperer creates **resilient, application-layer bridges** for specific ports. It encapsulates traffic in standard web protocols (HTTPS/WebSocket) to ensure connectivity even in environments with aggressive firewalls and Deep Packet Inspection (DPI).

---

## The Problem: "The Connectivity Gap"

In modern enterprise environments (Healthcare, Manufacturing, Finance), "getting things done" is often blocked by rigid network policies:

1. **Strict Egress Rules:** Firewalls often block everything except Port 443 (HTTPS). Standard tools like SSH (Port 22) or OpenVPN are dropped.
2. **Legacy Devices:** MRI scanners, industrial PLCs, and old servers often cannot install modern VPN clients (Tailscale/ZeroTier).
3. **DPI Interference:** sophisticated "Next-Gen" firewalls analyze traffic packet headers. They can detect and kill non-web traffic even if it tries to use Port 443.

**Tunnel Whisperer bridges this gap.** It wraps your traffic (SSH, TCP) inside a genuine TLS-encrypted WebSocket stream. To the network, your data transfer looks exactly like a long-lived connection to a standard website, ensuring high reliability without policy violations.

---

## Use Cases: Getting Work Done

### 🏥 1. Healthcare Interoperability (DICOM/HL7)

**Scenario:** You need to send X-Ray images (DICOM) from a hospital scanner to a cloud-based AI analysis platform.

* **The Friction:** Hospital IT requires 6 months to approve a Site-to-Site VPN. The MRI machine is a "black box" appliance that you can't install software on.

* **The Solution:** Connect a small gateway device (e.g., Raspberry Pi) to the scanner's LAN. Tunnel Whisperer forwards the local DICOM port (104) to your cloud endpoint via outbound HTTPS.

* **Result:** Instant, secure connectivity. The scanner sends data to "localhost", and the tunnel delivers it to the cloud.

### 🏭 2. Vendor Remote Support (OT/IoT)

**Scenario:** An industrial machine (PLC) on a factory floor is malfunctioning. The vendor needs to access the control interface (Modbus or HTTP) to diagnose it.

* **The Friction:** The factory network has no inbound access. Giving the vendor full VPN access to the factory floor is a security risk.

* **The Solution:** The factory deploys a Tunnel Whisperer agent that exposes *only* the specific maintenance port of that one machine.

* **Result:** The vendor gets surgical access to fix the issue without touching the rest of the network.

### 💻 3. Developer & Data Science Workflows

**Scenario:** A data scientist needs to query a sensitive on-premise database from a Jupyter Notebook running in the cloud.

* **The Friction:** The database is behind a corporate firewall that blocks direct SQL connections.

* **The Solution:** Tunnel Whisperer maps the database port (e.g., 5432) to the cloud environment via a resilient tunnel.

* **Result:** The notebook connects to `localhost:5432` as if the database were local.

---

## Architecture

Tunnel Whisperer combines the reliability of **OpenSSH** with the resilience of **V2Ray (VMess/VLESS)** transport.

1. **Transport Layer:** Traffic is encapsulated in **WebSocket + TLS**. This ensures compatibility with standard HTTPS proxies, CDNs (Cloudflare), and strict firewalls.
2. **Rendezvous Server:** A lightweight relay you host (VPS/Cloud). It presents a standard web server face (Nginx/Caddy) to the public internet, handling traffic routing only on specific, authenticated paths.
3. **Tunnel Layer:** Inside the resilient stream, OpenSSH handles port forwarding, encryption, and authentication.

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
