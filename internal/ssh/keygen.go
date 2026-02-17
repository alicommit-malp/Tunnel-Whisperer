package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"

	gossh "golang.org/x/crypto/ssh"
)

// GenerateKeyPair creates a new ed25519 SSH key pair.
// Returns the private key in PEM format and the public key in authorized_keys format.
func GenerateKeyPair() (privateKeyPEM []byte, publicKeyAuthorized []byte, err error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, nil, err
	}

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	})

	sshPub, err := gossh.NewPublicKey(pubKey)
	if err != nil {
		return nil, nil, err
	}

	authorizedKey := gossh.MarshalAuthorizedKey(sshPub)

	return privPEM, authorizedKey, nil
}
