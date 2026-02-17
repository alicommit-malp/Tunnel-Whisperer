package ssh

import (
	"fmt"
	"log"
)

// Client manages SSH connections and port forwarding.
type Client struct {
	Host       string
	Port       int
	User       string
	PrivateKey []byte
}

func NewClient(host string, port int, user string, privateKey []byte) *Client {
	return &Client{
		Host:       host,
		Port:       port,
		User:       user,
		PrivateKey: privateKey,
	}
}

// Connect establishes an SSH connection to the remote host.
func (c *Client) Connect() error {
	log.Printf("ssh: connecting to %s:%d as %s", c.Host, c.Port, c.User)
	return fmt.Errorf("ssh client: Connect not yet implemented")
}

// ReverseForward sets up reverse port forwarding (ssh -R).
func (c *Client) ReverseForward(remotePort, localPort int) error {
	log.Printf("ssh: reverse forward %d -> localhost:%d", remotePort, localPort)
	return fmt.Errorf("ssh client: ReverseForward not yet implemented")
}
