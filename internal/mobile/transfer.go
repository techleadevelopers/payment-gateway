package mobile

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const (
	bscUSDCContractMobile     = "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d"
	polygonUSDCContractMobile = "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174"
	defaultPolygonUSDTMobile  = "0xc2132D05D31c914a87C6611C10748AEb04B58e8F"
)

func (s *Server) handleWalletTransfer(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	user, err := mobileDB(s.db).GetUserByID(r.Context(), uid)
	if err != nil || user == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "usuario nao encontrado"})
		return
	}
	if user.WalletAddress == nil || strings.TrimSpace(*user.WalletAddress) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "wallet do usuario nao registrada"})
		return
	}

	var req struct {
		To      string `json:"to"`
		Amount  string `json:"amount"`
		Asset   string `json:"asset"`
		Network string `json:"network"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "payload invalido"})
		return
	}

	to := strings.TrimSpace(req.To)
	if !common.IsHexAddress(to) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "to deve ser um endereco EVM valido"})
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(req.Asset))
	if asset == "" {
		asset = "USDT"
	}
	network := normalizeMobileTransferNetwork(req.Network)
	if network == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "network deve ser BSC ou POLYGON"})
		return
	}

	token, decimals, chainID, err := s.mobileTransferToken(asset, network)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	rawAmount, err := parseTokenAmount(req.Amount, decimals)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":             "client_signed_erc20_transfer",
		"from":             common.HexToAddress(*user.WalletAddress).Hex(),
		"to":               common.HexToAddress(token).Hex(),
		"value":            "0x0",
		"data":             erc20TransferCalldata(common.HexToAddress(to), rawAmount),
		"chainId":          chainID,
		"network":          network,
		"asset":            asset,
		"token_contract":   common.HexToAddress(token).Hex(),
		"recipient":        common.HexToAddress(to).Hex(),
		"amount":           req.Amount,
		"amount_raw":       rawAmount.String(),
		"decimals":         decimals,
		"signing_required": true,
		"next_step":        "Assine e envie esta transacao na wallet client-side. A ChainFX nao recebe nem armazena private key.",
	})
}

func normalizeMobileTransferNetwork(network string) string {
	switch strings.ToUpper(strings.TrimSpace(network)) {
	case "", "BSC", "BINANCE", "BEP20":
		return "BSC"
	case "POL", "POLYGON", "MATIC":
		return "POLYGON"
	default:
		return ""
	}
}

func (s *Server) mobileTransferToken(asset, network string) (string, int, int, error) {
	switch network {
	case "BSC":
		switch asset {
		case "USDT":
			if s.cfg == nil || !common.IsHexAddress(s.cfg.BscUsdtContract) {
				return "", 0, 0, fmt.Errorf("BSC USDT nao configurado")
			}
			return s.cfg.BscUsdtContract, 18, 56, nil
		case "USDC":
			return bscUSDCContractMobile, 18, 56, nil
		}
	case "POLYGON":
		switch asset {
		case "USDT":
			token := defaultPolygonUSDTMobile
			if s.cfg != nil && common.IsHexAddress(s.cfg.PolygonUsdtContract) {
				token = s.cfg.PolygonUsdtContract
			}
			return token, 6, 137, nil
		case "USDC":
			return polygonUSDCContractMobile, 6, 137, nil
		}
	}
	return "", 0, 0, fmt.Errorf("asset/network nao suportado para transferencia mobile")
}

func parseTokenAmount(amount string, decimals int) (*big.Int, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return nil, fmt.Errorf("amount obrigatorio")
	}
	rat, ok := new(big.Rat).SetString(amount)
	if !ok || rat.Sign() <= 0 {
		return nil, fmt.Errorf("amount deve ser positivo")
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	rat.Mul(rat, new(big.Rat).SetInt(scale))
	if !rat.IsInt() {
		return nil, fmt.Errorf("amount tem mais casas decimais que o token suporta")
	}
	return rat.Num(), nil
}

func erc20TransferCalldata(to common.Address, amount *big.Int) string {
	selector := []byte{0xa9, 0x05, 0x9c, 0xbb}
	data := make([]byte, 0, 68)
	data = append(data, selector...)
	data = append(data, common.LeftPadBytes(to.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	return "0x" + hex.EncodeToString(data)
}
