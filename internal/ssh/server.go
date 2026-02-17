package ssh

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	gossh "golang.org/x/crypto/ssh"
)

// Server is an embedded SSH server used for relay-to-server connectivity.
type Server struct {
	Port           int
	HostKeyDir     string
	AuthorizedKeys string
	config         *gossh.ServerConfig
	listener       net.Listener
}

func NewServer(port int, hostKeyDir, authorizedKeys string) (*Server, error) {
	s := &Server{
		Port:           port,
		HostKeyDir:     hostKeyDir,
		AuthorizedKeys: authorizedKeys,
		config:         &gossh.ServerConfig{},
	}

	if err := s.loadAuthorizedKeys(); err != nil {
		return nil, err
	}

	if err := s.loadOrGenerateHostKey(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Server) loadAuthorizedKeys() error {
	data, err := os.ReadFile(s.AuthorizedKeys)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("ssh-server: no authorized_keys file at %s — no clients can connect until one is created", s.AuthorizedKeys)
			s.config.NoClientAuth = false
			s.config.PublicKeyCallback = func(conn gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
				return nil, fmt.Errorf("no authorized keys configured")
			}
			return nil
		}
		return fmt.Errorf("reading authorized_keys: %w", err)
	}

	var allowed []gossh.PublicKey
	rest := data
	for len(rest) > 0 {
		pub, _, _, r, err := gossh.ParseAuthorizedKey(rest)
		if err != nil {
			break
		}
		allowed = append(allowed, pub)
		rest = r
	}

	log.Printf("ssh-server: loaded %d authorized key(s) from %s", len(allowed), s.AuthorizedKeys)

	s.config.PublicKeyCallback = func(conn gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
		keyBytes := key.Marshal()
		for _, ak := range allowed {
			if string(ak.Marshal()) == string(keyBytes) {
				log.Printf("ssh-server: authenticated user %q from %s", conn.User(), conn.RemoteAddr())
				return &gossh.Permissions{}, nil
			}
		}
		return nil, fmt.Errorf("unknown public key for %q", conn.User())
	}

	return nil
}

func (s *Server) loadOrGenerateHostKey() error {
	keyPath := filepath.Join(s.HostKeyDir, "ssh_host_ed25519_key")

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading host key: %w", err)
		}

		log.Println("ssh-server: generating host key...")
		if err := os.MkdirAll(s.HostKeyDir, 0700); err != nil {
			return fmt.Errorf("creating host key directory: %w", err)
		}

		privPEM, _, err := GenerateKeyPair()
		if err != nil {
			return fmt.Errorf("generating host key: %w", err)
		}
		if err := os.WriteFile(keyPath, privPEM, 0600); err != nil {
			return fmt.Errorf("writing host key: %w", err)
		}
		keyData = privPEM
	}

	signer, err := gossh.ParsePrivateKey(keyData)
	if err != nil {
		return fmt.Errorf("parsing host key: %w", err)
	}

	s.config.AddHostKey(signer)
	return nil
}

// Run starts the SSH server (blocking).
func (s *Server) Run() error {
	addr := fmt.Sprintf(":%d", s.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ssh-server: listen %s: %w", addr, err)
	}
	s.listener = lis

	log.Printf("ssh-server: listening on %s", addr)

	for {
		conn, err := lis.Accept()
		if err != nil {
			return fmt.Errorf("ssh-server: accept: %w", err)
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	sshConn, chans, reqs, err := gossh.NewServerConn(conn, s.config)
	if err != nil {
		log.Printf("ssh-server: handshake failed: %v", err)
		return
	}
	defer sshConn.Close()

	log.Printf("ssh-server: connection from %s (%s) user=%s", sshConn.RemoteAddr(), sshConn.ClientVersion(), sshConn.User())

	go gossh.DiscardRequests(reqs)

	for newChan := range chans {
		switch newChan.ChannelType() {
		case "direct-tcpip":
			go s.handleDirectTCPIP(newChan)
		default:
			newChan.Reject(gossh.UnknownChannelType, fmt.Sprintf("unsupported channel type: %s", newChan.ChannelType()))
		}
	}
}

// directTCPIPData matches the RFC 4254 §7.2 payload for direct-tcpip channels.
type directTCPIPData struct {
	DestHost   string
	DestPort   uint32
	OriginHost string
	OriginPort uint32
}

func parseDirectTCPIP(data []byte) (directTCPIPData, error) {
	var d directTCPIPData
	if len(data) < 4 {
		return d, fmt.Errorf("data too short")
	}

	hostLen := binary.BigEndian.Uint32(data[0:4])
	if uint32(len(data)) < 4+hostLen+4+4+4 {
		return d, fmt.Errorf("data too short for dest host")
	}
	d.DestHost = string(data[4 : 4+hostLen])
	offset := 4 + hostLen
	d.DestPort = binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4

	origHostLen := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	if uint32(len(data)) < offset+origHostLen+4 {
		return d, fmt.Errorf("data too short for origin host")
	}
	d.OriginHost = string(data[offset : offset+origHostLen])
	offset += origHostLen
	d.OriginPort = binary.BigEndian.Uint32(data[offset : offset+4])

	return d, nil
}

func (s *Server) handleDirectTCPIP(newChan gossh.NewChannel) {
	d, err := parseDirectTCPIP(newChan.ExtraData())
	if err != nil {
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("invalid direct-tcpip data: %v", err))
		return
	}

	dest := net.JoinHostPort(d.DestHost, fmt.Sprintf("%d", d.DestPort))
	log.Printf("ssh-server: direct-tcpip %s:%d → %s", d.OriginHost, d.OriginPort, dest)

	conn, err := net.Dial("tcp", dest)
	if err != nil {
		newChan.Reject(gossh.ConnectionFailed, fmt.Sprintf("dial %s: %v", dest, err))
		return
	}
	defer conn.Close()

	ch, _, err := newChan.Accept()
	if err != nil {
		log.Printf("ssh-server: channel accept: %v", err)
		return
	}
	defer ch.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(conn, ch)
	}()

	go func() {
		defer wg.Done()
		io.Copy(ch, conn)
	}()

	wg.Wait()
}

// Stop gracefully stops the SSH server.
func (s *Server) Stop() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
