package ssh

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// ForwardTunnel connects to a remote SSH server and sets up local
// port forwarding (-L) so that a local port forwards through SSH
// to a remote destination.
type ForwardTunnel struct {
	// Remote SSH server to connect to (via Xray tunnel).
	RemoteAddr string
	// SSH user on the remote server.
	User string
	// Path to the private key for authentication.
	KeyPath string
	// Port on localhost to listen on.
	LocalPort int
	// Remote host to forward to (from the SSH server's perspective).
	RemoteHost string
	// Remote port to forward to.
	RemotePort int

	mu       sync.Mutex
	client   *gossh.Client
	listener net.Listener
	done     chan struct{}
}

// Run connects to the remote SSH server, listens locally, and forwards
// connections through SSH. It automatically reconnects with exponential
// backoff on failure.
func (ft *ForwardTunnel) Run() error {
	ft.done = make(chan struct{})
	backoff := time.Second * 2

	for {
		select {
		case <-ft.done:
			return nil
		default:
		}

		err := ft.connect()
		if err != nil {
			log.Printf("forward-tunnel: connection failed: %v", err)
		} else {
			backoff = time.Second * 2
		}

		// Clean up before reconnecting.
		ft.cleanup()

		select {
		case <-ft.done:
			return nil
		case <-time.After(backoff):
			log.Printf("forward-tunnel: reconnecting (backoff %s)...", backoff)
		}

		// Exponential backoff: 2s → 4s → 8s → 16s → 30s (max).
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

// cleanup closes the current listener and SSH client so ports are freed
// for the next reconnection attempt.
func (ft *ForwardTunnel) cleanup() {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	if ft.listener != nil {
		ft.listener.Close()
		ft.listener = nil
	}
	if ft.client != nil {
		ft.client.Close()
		ft.client = nil
	}
}

func (ft *ForwardTunnel) connect() error {
	keyData, err := os.ReadFile(ft.KeyPath)
	if err != nil {
		return fmt.Errorf("reading private key: %w", err)
	}

	signer, err := gossh.ParsePrivateKey(keyData)
	if err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}

	sshConfig := &gossh.ClientConfig{
		User: ft.User,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(signer),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	log.Printf("forward-tunnel: connecting to %s as %s", ft.RemoteAddr, ft.User)

	conn, err := net.DialTimeout("tcp", ft.RemoteAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", ft.RemoteAddr, err)
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(30 * time.Second)
	}

	sshConn, chans, reqs, err := gossh.NewClientConn(conn, ft.RemoteAddr, sshConfig)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SSH handshake: %w", err)
	}

	ft.mu.Lock()
	ft.client = gossh.NewClient(sshConn, chans, reqs)
	ft.mu.Unlock()

	// Start SSH keepalive — on failure it closes both the SSH connection
	// AND the local listener so connect() can return and the reconnect
	// loop fires.
	go ft.keepalive(sshConn)

	// Listen locally.
	listenAddr := fmt.Sprintf("127.0.0.1:%d", ft.LocalPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		ft.client.Close()
		return fmt.Errorf("listening on %s: %w", listenAddr, err)
	}

	ft.mu.Lock()
	ft.listener = listener
	ft.mu.Unlock()

	log.Printf("forward-tunnel: localhost:%d → %s:%d (via SSH)", ft.LocalPort, ft.RemoteHost, ft.RemotePort)

	for {
		local, err := listener.Accept()
		if err != nil {
			select {
			case <-ft.done:
				return nil
			default:
			}
			return fmt.Errorf("accepting connection: %w", err)
		}

		go ft.forward(local)
	}
}

// keepalive sends periodic SSH keepalive requests to detect dead connections.
// On failure, it closes both the SSH connection AND the local listener so
// that connect() unblocks and the reconnect loop fires.
func (ft *ForwardTunnel) keepalive(conn gossh.Conn) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ft.done:
			return
		case <-ticker.C:
			_, _, err := conn.SendRequest("keepalive@tw", true, nil)
			if err != nil {
				log.Printf("forward-tunnel: keepalive failed, triggering reconnect: %v", err)
				// Close listener first — this unblocks Accept() in connect().
				ft.mu.Lock()
				if ft.listener != nil {
					ft.listener.Close()
				}
				ft.mu.Unlock()
				conn.Close()
				return
			}
		}
	}
}

func (ft *ForwardTunnel) forward(local net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("forward-tunnel: panic in forward: %v", r)
		}
	}()
	defer local.Close()

	ft.mu.Lock()
	client := ft.client
	ft.mu.Unlock()

	if client == nil {
		log.Printf("forward-tunnel: no SSH client available")
		return
	}

	remoteAddr := fmt.Sprintf("%s:%d", ft.RemoteHost, ft.RemotePort)
	remote, err := client.Dial("tcp", remoteAddr)
	if err != nil {
		log.Printf("forward-tunnel: failed to dial %s via SSH: %v", remoteAddr, err)
		return
	}
	defer remote.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(remote, local)
		if tc, ok := remote.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(local, remote)
		if tc, ok := local.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
}

// Stop shuts down the forward tunnel.
func (ft *ForwardTunnel) Stop() {
	if ft.done != nil {
		close(ft.done)
	}
	ft.cleanup()
}
