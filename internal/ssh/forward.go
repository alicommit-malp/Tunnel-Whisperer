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

// Mapping defines a single local-port → remote-host:port forwarding rule.
type Mapping struct {
	LocalPort  int
	RemoteHost string
	RemotePort int
}

// ForwardTunnel connects to a remote SSH server and sets up multiple local
// port forwards (-L) over a single SSH session.
type ForwardTunnel struct {
	// Remote SSH server to connect to (via Xray tunnel).
	RemoteAddr string
	// SSH user on the remote server.
	User string
	// Path to the private key for authentication.
	KeyPath string
	// Port mappings to forward.
	Mappings []Mapping

	mu        sync.Mutex
	client    *gossh.Client
	listeners []net.Listener
	done      chan struct{}
}

// Run connects to the remote SSH server, starts all local listeners, and
// forwards connections through SSH. It automatically reconnects with
// exponential backoff on failure.
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

// cleanup closes all listeners and the SSH client so ports are freed
// for the next reconnection attempt.
func (ft *ForwardTunnel) cleanup() {
	ft.mu.Lock()
	defer ft.mu.Unlock()

	for _, l := range ft.listeners {
		l.Close()
	}
	ft.listeners = nil

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

	// Start SSH keepalive — on failure it closes all listeners and the SSH
	// connection so connect() returns and the reconnect loop fires.
	go ft.keepalive(sshConn)

	// Start a local listener for each mapping.
	// All listeners share the same SSH client.
	acceptDone := make(chan struct{})
	var wg sync.WaitGroup

	for _, m := range ft.Mappings {
		listenAddr := fmt.Sprintf("127.0.0.1:%d", m.LocalPort)
		listener, err := net.Listen("tcp", listenAddr)
		if err != nil {
			close(acceptDone)
			wg.Wait()
			ft.client.Close()
			return fmt.Errorf("listening on %s: %w", listenAddr, err)
		}

		ft.mu.Lock()
		ft.listeners = append(ft.listeners, listener)
		ft.mu.Unlock()

		log.Printf("forward-tunnel: localhost:%d → %s:%d (via SSH)", m.LocalPort, m.RemoteHost, m.RemotePort)

		wg.Add(1)
		go func(l net.Listener, m Mapping) {
			defer wg.Done()
			ft.acceptLoop(l, m, acceptDone)
		}(listener, m)
	}

	// Block until all accept loops finish (triggered by keepalive failure or Stop).
	wg.Wait()

	select {
	case <-ft.done:
		return nil
	default:
		return fmt.Errorf("all listeners closed")
	}
}

// acceptLoop accepts connections on a listener and forwards them through SSH.
func (ft *ForwardTunnel) acceptLoop(listener net.Listener, m Mapping, done <-chan struct{}) {
	for {
		local, err := listener.Accept()
		if err != nil {
			select {
			case <-ft.done:
			case <-done:
			default:
				log.Printf("forward-tunnel: accept error on :%d: %v", m.LocalPort, err)
			}
			return
		}

		go ft.forward(local, m)
	}
}

// keepalive sends periodic SSH keepalive requests to detect dead connections.
// On failure, it closes all listeners and the SSH connection so that
// connect() unblocks and the reconnect loop fires.
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
				// Close listeners first — this unblocks Accept() in all loops.
				ft.mu.Lock()
				for _, l := range ft.listeners {
					l.Close()
				}
				ft.mu.Unlock()
				conn.Close()
				return
			}
		}
	}
}

func (ft *ForwardTunnel) forward(local net.Conn, m Mapping) {
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

	remoteAddr := fmt.Sprintf("%s:%d", m.RemoteHost, m.RemotePort)
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
