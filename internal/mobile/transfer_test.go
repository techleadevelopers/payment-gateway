package mobile

import (
	"math/big"
	"strings"
	"testing"

	"payment-gateway/internal/config"

	"github.com/ethereum/go-ethereum/common"
)

func TestParseTokenAmount(t *testing.T) {
	tests := []struct {
		name     string
		amount   string
		decimals int
		want     string
		wantErr  bool
	}{
		{name: "bsc decimals", amount: "1.25", decimals: 18, want: "1250000000000000000"},
		{name: "polygon decimals", amount: "1.25", decimals: 6, want: "1250000"},
		{name: "too many decimals", amount: "0.0000001", decimals: 6, wantErr: true},
		{name: "zero", amount: "0", decimals: 18, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTokenAmount(tt.amount, tt.decimals)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %s, want %s", got.String(), tt.want)
			}
		})
	}
}

func TestERC20TransferCalldata(t *testing.T) {
	to := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc454e4438f44e")
	data := erc20TransferCalldata(to, big.NewInt(1000000))

	if !strings.HasPrefix(data, "0xa9059cbb") {
		t.Fatalf("missing transfer selector: %s", data)
	}
	if len(data) != 138 {
		t.Fatalf("unexpected calldata length %d", len(data))
	}
	if !strings.Contains(strings.ToLower(data), strings.TrimPrefix(strings.ToLower(to.Hex()), "0x")) {
		t.Fatalf("recipient missing from calldata: %s", data)
	}
}

func TestMobileTransferTokenUsesConfiguredChainID(t *testing.T) {
	s := &Server{cfg: &config.Config{
		BscUsdtContract:     "0x0000000000000000000000000000000000000001",
		PolygonUsdtContract: "0x0000000000000000000000000000000000000002",
		BscChainID:          97,
		PolygonChainID:      80002,
	}}

	_, _, bscChainID, err := s.mobileTransferToken("USDT", "BSC")
	if err != nil {
		t.Fatalf("unexpected BSC error: %v", err)
	}
	if bscChainID != 97 {
		t.Fatalf("BSC chainID = %d, want 97", bscChainID)
	}

	_, _, polygonChainID, err := s.mobileTransferToken("USDT", "POLYGON")
	if err != nil {
		t.Fatalf("unexpected Polygon error: %v", err)
	}
	if polygonChainID != 80002 {
		t.Fatalf("Polygon chainID = %d, want 80002", polygonChainID)
	}
}

func TestMobileTransferTokenDefaultsToMainnetChainID(t *testing.T) {
	s := &Server{cfg: &config.Config{
		BscUsdtContract:     "0x0000000000000000000000000000000000000001",
		PolygonUsdtContract: "0x0000000000000000000000000000000000000002",
	}}

	_, _, bscChainID, err := s.mobileTransferToken("USDT", "BSC")
	if err != nil {
		t.Fatalf("unexpected BSC error: %v", err)
	}
	if bscChainID != 56 {
		t.Fatalf("BSC chainID = %d, want 56", bscChainID)
	}

	_, _, polygonChainID, err := s.mobileTransferToken("USDT", "POLYGON")
	if err != nil {
		t.Fatalf("unexpected Polygon error: %v", err)
	}
	if polygonChainID != 137 {
		t.Fatalf("Polygon chainID = %d, want 137", polygonChainID)
	}
}

func TestMobileTransferTokenSupportsNewEVMUSDCNetworks(t *testing.T) {
	s := &Server{cfg: &config.Config{
		BaseUsdcContract:     "0x0000000000000000000000000000000000000003",
		BaseChainID:          84531,
		ArbitrumUsdcContract: "0x0000000000000000000000000000000000000004",
		ArbitrumChainID:      421614,
		EthereumUsdcContract: "0x0000000000000000000000000000000000000005",
		EthereumChainID:      11155111,
	}}

	tests := []struct {
		network string
		want    int
	}{
		{"BASE", 84531},
		{"ARBITRUM", 421614},
		{"ETHEREUM", 11155111},
	}
	for _, tt := range tests {
		t.Run(tt.network, func(t *testing.T) {
			token, decimals, chainID, err := s.mobileTransferToken("USDC", tt.network)
			if err != nil {
				t.Fatalf("mobileTransferToken: %v", err)
			}
			if token == "" || decimals != 6 || chainID != tt.want {
				t.Fatalf("token=%q decimals=%d chainID=%d, want configured USDC/6/%d", token, decimals, chainID, tt.want)
			}
		})
	}
}

func TestNormalizeMobileTransferNetworkRejectsNonEVMRails(t *testing.T) {
	for _, network := range []string{"BITCOIN", "SOLANA", "APTOS"} {
		if got := normalizeMobileTransferNetwork(network); got != "" {
			t.Fatalf("normalizeMobileTransferNetwork(%q)=%q, want empty", network, got)
		}
	}
}
