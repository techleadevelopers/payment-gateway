package server

import (
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

func (s *Server) deliveryNetwork() string {
	network := strings.ToUpper(strings.TrimSpace(s.cfg.SignerNetwork))
	switch network {
	case "", "EVM", "BINANCE", "BEP20":
		return "BSC"
	default:
		return network
	}
}

func normalizeSellNetwork(network string) string {
	switch strings.ToUpper(strings.TrimSpace(network)) {
	case "", "BSC", "BINANCE", "BEP20":
		return "BSC"
	case "POL", "POLYGON", "MATIC":
		return "POLYGON"
	default:
		return strings.ToUpper(strings.TrimSpace(network))
	}
}

func (s *Server) supportedSellNetworks() []string {
	networks := []string{}
	if strings.TrimSpace(s.cfg.BscRpcUrls) != "" && strings.TrimSpace(s.cfg.BscUsdtContract) != "" {
		networks = append(networks, "BSC")
	}
	if strings.TrimSpace(s.cfg.PolygonRpcUrls) != "" && strings.TrimSpace(s.cfg.PolygonUsdtContract) != "" {
		networks = append(networks, "POLYGON")
	}
	if len(networks) == 0 {
		networks = append(networks, "BSC")
	}
	return networks
}

func (s *Server) sellNetworkEnabled(network string) bool {
	switch normalizeSellNetwork(network) {
	case "BSC":
		return true
	case "POLYGON":
		return strings.TrimSpace(s.cfg.PolygonRpcUrls) != "" && strings.TrimSpace(s.cfg.PolygonUsdtContract) != ""
	default:
		return false
	}
}

func (s *Server) isDeliveryAddress(address string) bool {
	address = strings.TrimSpace(address)
	switch s.deliveryNetwork() {
	case "BSC", "EVM":
		return common.IsHexAddress(address)
	default:
		return false
	}
}

func normalizePaymentRail(currency, method string, amountFiat, amountBRL, amountUSD float64) (string, string, float64) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	method = strings.ToLower(strings.TrimSpace(method))
	if currency == "" {
		if amountUSD > 0 {
			currency = "USD"
		} else {
			currency = "BRL"
		}
	}
	if method == "" {
		if currency == "USD" {
			method = "stripe"
		} else {
			method = "pix"
		}
	}
	if amountFiat <= 0 {
		if currency == "USD" {
			amountFiat = amountUSD
		} else {
			amountFiat = amountBRL
		}
	}
	switch {
	case currency == "BRL" && method == "pix":
		return currency, method, amountFiat
	case currency == "USD" && method == "stripe":
		return currency, method, amountFiat
	default:
		return "", "", 0
	}
}
