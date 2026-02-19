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

// ReverseTunnel connects to a remote SSH server and sets up a reverse
// port forward (-R) so that remote clients can reach a local port.
type ReverseTunnel struct {
	// Remote SSH server to connect to (via Xray tunnel).
	RemoteAddr string
	// SSH user on the remote server.
	User string
	// Path to the private key for authentication.
	KeyPath string
	// Port on the remote server to listen on.
	RemotePort int
	// Local address to forward to (e.g. "127.0.0.1:2222").
	LocalAddr string

	client *gossh.Client
	done   chan struct{}
}

// Run connects to the remote SSH server, sets up the reverse port
// forward, and blocks until the tunnel is closed or an error occurs.
// It automatically reconnects with exponential backoff on failure.
func (rt *ReverseTunnel) Run() error {
	rt.done = make(chan struct{})
	backoff := time.Second * 2

	for {
		select {
		case <-rt.done:
			return nil
		default:
		}

		err := rt.connect()
		if err != nil {
			log.Printf("reverse-tunnel: connection failed: %v", err)
		} else {
			// Successful connection resets backoff.
			backoff = time.Second * 2
		}

		select {
		case <-rt.done:
			return nil
		case <-time.After(backoff):
			log.Printf("reverse-tunnel: reconnecting (backoff %s)...", backoff)
		}

		// Exponential backoff: 2s → 4s → 8s → 16s → 30s (max).
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (rt *ReverseTunnel) connect() error {
	keyData, err := os.ReadFile(rt.KeyPath)
	if err != nil {
		return fmt.Errorf("reading private key: %w", err)
	}

	signer, err := gossh.ParsePrivateKey(keyData)
	if err != nil {
		return fmt.Errorf("parsing private key: %w", err)
	}

	config := &gossh.ClientConfig{
		User: rt.User,
		Auth: []gossh.AuthMethod{
			gossh.PublicKeys(signer),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	log.Printf("reverse-tunnel: connecting to %s as %s", rt.RemoteAddr, rt.User)

	conn, err := net.DialTimeout("tcp", rt.RemoteAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dialing %s: %w", rt.RemoteAddr, err)
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(30 * time.Second)
	}

	sshConn, chans, reqs, err := gossh.NewClientConn(conn, rt.RemoteAddr, config)
	if err != nil {
		conn.Close()
		return fmt.Errorf("SSH handshake: %w", err)
	}

	rt.client = gossh.NewClient(sshConn, chans, reqs)

	// Start SSH keepalive in background.
	go rt.keepalive(sshConn)

	// Request reverse port forward.
	listener, err := rt.client.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", rt.RemotePort))
	if err != nil {
		rt.client.Close()
		return fmt.Errorf("requesting reverse forward on :%d: %w", rt.RemotePort, err)
	}
	defer listener.Close()

	log.Printf("reverse-tunnel: listening on relay :%d → %s", rt.RemotePort, rt.LocalAddr)

	for {
		remote, err := listener.Accept()
		if err != nil {
			select {
			case <-rt.done:
				return nil
			default:
			}
			return fmt.Errorf("accepting reverse connection: %w", err)
		}

		go rt.forward(remote)
	}
}

// keepalive sends periodic SSH keepalive requests to detect dead connections.
func (rt *ReverseTunnel) keepalive(conn gossh.Conn) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rt.done:
			return
		case <-ticker.C:
			_, _, err := conn.SendRequest("keepalive@tw", true, nil)
			if err != nil {
				log.Printf("reverse-tunnel: keepalive failed: %v", err)
				conn.Close()
				return
			}
		}
	}
}

func (rt *ReverseTunnel) forward(remote net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("reverse-tunnel: panic in forward: %v", r)
		}
	}()
	defer remote.Close()

	local, err := net.DialTimeout("tcp", rt.LocalAddr, 10*time.Second)
	if err != nil {
		log.Printf("reverse-tunnel: failed to connect to local %s: %v", rt.LocalAddr, err)
		return
	}
	defer local.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(local, remote)
		if tc, ok := local.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(remote, local)
		if tc, ok := remote.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
}

// Stop shuts down the reverse tunnel.
func (rt *ReverseTunnel) Stop() {
	if rt.done != nil {
		close(rt.done)
	}
	if rt.client != nil {
		rt.client.Close()
	}
}
