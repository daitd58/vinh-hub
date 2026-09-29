package fund

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	mmn "github.com/mezonai/mmn-sdk/go-sdk/client"
)

// NodeSource reads wallet balances directly from the mmn node via gRPC.
type NodeSource struct {
	endpoint string
	useTLS   bool
	mu       sync.Mutex
	client   *mmn.MmnClient
}

// NewNodeSource creates a Source backed by the mmn node gRPC client.
func NewNodeSource(endpoint string, useTLS bool) *NodeSource {
	return &NodeSource{
		endpoint: endpoint,
		useTLS:   useTLS,
	}
}

func (n *NodeSource) getClient() (*mmn.MmnClient, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.client != nil {
		return n.client, nil
	}
	c, err := mmn.NewClient(mmn.Config{
		Endpoint: n.endpoint,
		UseTLS:   n.useTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to mmn node %s: %w", n.endpoint, err)
	}
	n.client = c
	return n.client, nil
}

// Close releases the gRPC connection if open.
func (n *NodeSource) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.client != nil {
		_ = n.client.Close()
		n.client = nil
	}
}

// WalletBalance queries the live on-chain balance of an address directly from the node.
func (n *NodeSource) WalletBalance(ctx context.Context, address string) (*big.Int, error) {
	c, err := n.getClient()
	if err != nil {
		return nil, err
	}

	acc, err := c.GetAccount(ctx, address)
	if err != nil {
		// Reset client on error so next invocation reconnects
		n.mu.Lock()
		if n.client != nil {
			_ = n.client.Close()
			n.client = nil
		}
		n.mu.Unlock()
		return nil, fmt.Errorf("node get account %s: %w", address, err)
	}

	if acc.Balance == nil {
		return big.NewInt(0), nil
	}
	return new(big.Int).SetBytes(acc.Balance.Bytes()), nil
}

// FallbackSource queries primary source first and falls back to secondary if primary fails.
type FallbackSource struct {
	Primary   Source
	Secondary Source
}

// WalletBalance attempts to read the balance from Primary, falling back to Secondary.
func (f *FallbackSource) WalletBalance(ctx context.Context, address string) (*big.Int, error) {
	if f.Primary != nil {
		bal, err := f.Primary.WalletBalance(ctx, address)
		if err == nil && bal != nil {
			return bal, nil
		}
	}
	if f.Secondary != nil {
		return f.Secondary.WalletBalance(ctx, address)
	}
	return nil, fmt.Errorf("no balance source available")
}
