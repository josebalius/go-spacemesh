package node

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// LocalNode is a public-private key pair used locally.
type LocalNode struct {
	publicKey p2pcrypto.PublicKey
	privKey   p2pcrypto.PrivateKey
}

// PublicKey returns the node's public key.
func (n LocalNode) PublicKey() p2pcrypto.PublicKey {
	return n.publicKey
}

// PrivateKey returns this node's private key.
func (n LocalNode) PrivateKey() p2pcrypto.PrivateKey {
	return n.privKey
}

// NewNodeIdentity creates a new local node without attempting to restore node from local store.
func NewNodeIdentity() (ln LocalNode, err error) {
	priv, pub, err := p2pcrypto.GenerateKeyPair()
	if err != nil {
		return ln, err
	}
	return LocalNode{
		publicKey: pub,
		privKey:   priv,
	}, nil
}

// Creates a new node from persisted NodeData.
func newLocalNodeFromFile(d *nodeFileData) (ln LocalNode, err error) {
	priv, err := p2pcrypto.NewPrivateKeyFromBase58(d.PrivKey)
	if err != nil {
		return ln, fmt.Errorf("failed to parse private key: %w", err)
	}

	pub, err := p2pcrypto.NewPublicKeyFromBase58(d.PubKey)
	if err != nil {
		return ln, fmt.Errorf("failed to parse public key: %w", err)
	}

	n := LocalNode{
		publicKey: pub,
		privKey:   priv,
	}

	return n, nil
}
