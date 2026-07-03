package tron

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"payment-gateway/internal/config"
)

type Client struct {
	cfg *config.Config
}

func NewClient(cfg *config.Config) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) DeriveAddress(index int) (string, error) {
	if index < 0 {
		return "", fmt.Errorf("índice de derivação inválido")
	}
	seed := c.cfg.TronXPub
	if seed == "" {
		seed = "dev-xpub-placeholder"
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", seed, index)))
	encoded := base64.RawStdEncoding.EncodeToString(sum[:])
	encoded = strings.NewReplacer("+", "A", "/", "B").Replace(encoded)
	if len(encoded) < 33 {
		return "", fmt.Errorf("falha ao derivar endereço")
	}
	return "T" + encoded[:33], nil
}

func IsAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	return len(addr) >= 26 && len(addr) <= 42 && strings.HasPrefix(addr, "T")
}
