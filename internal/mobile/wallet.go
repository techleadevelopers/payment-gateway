package mobile

import (
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

func (s *Server) handleWalletBalance(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	user, err := mobileDB(s.db).GetUserByID(r.Context(), uid)
	if err != nil || user == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "usuario nao encontrado"})
		return
	}

	walletAddr := ""
	if user.WalletAddress != nil {
		walletAddr = *user.WalletAddress
	}
	price := mobileAssetPriceBRL(s.PriceCache(), "USDT")
	writeJSON(w, http.StatusOK, map[string]any{
		"wallet_address": walletAddr,
		"balances": []map[string]any{
			{"symbol": "USDT", "network": "BSC", "amount": 0, "value_brl": 0},
			{"symbol": "BNB", "network": "BSC", "amount": 0, "value_brl": 0},
		},
		"total_brl":  0,
		"price_usdt": price,
	})
}

func (s *Server) handleWalletTokens(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", mobileRateCacheControl)
	price := mobileAssetPriceBRL(s.PriceCache(), "USDT")
	writeJSON(w, http.StatusOK, map[string]any{
		"tokens": []map[string]any{
			{"symbol": "USDT", "name": "Tether USD", "network": "BSC", "contract": s.cfg.BscUsdtContract, "price_brl": price, "decimals": 18},
			{"symbol": "BNB", "name": "BNB", "network": "BSC", "contract": "", "price_brl": 0, "decimals": 18},
		},
	})
}

func (s *Server) handleWalletAddress(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	user, _ := mobileDB(s.db).GetUserByID(r.Context(), uid)
	if user != nil && user.WalletAddress != nil && *user.WalletAddress != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"wallet_address": *user.WalletAddress,
			"network":        "BSC",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"wallet_address": nil, "hint": "use POST /api/mobile/wallet/generate com wallet_address"})
}

func (s *Server) handleWalletGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WalletAddress string `json:"wallet_address"`
		Address       string `json:"address"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "wallet_address obrigatorio"})
		return
	}

	uid := userIDFromCtx(r)
	user, _ := mobileDB(s.db).GetUserByID(r.Context(), uid)
	if user != nil && user.WalletAddress != nil && *user.WalletAddress != "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "carteira ja registrada", "wallet_address": *user.WalletAddress})
		return
	}

	address := strings.TrimSpace(req.WalletAddress)
	if address == "" {
		address = strings.TrimSpace(req.Address)
	}
	if !common.IsHexAddress(address) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "wallet_address deve ser um endereco EVM valido"})
		return
	}
	checksummed := common.HexToAddress(address).Hex()

	if err := mobileDB(s.db).UpdateUser(r.Context(), uid, map[string]any{"wallet_address": checksummed}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"wallet_address": checksummed,
		"network":        "BSC",
		"custody":        "client",
		"message":        "wallet registrada; a private key deve permanecer somente no app/agente",
	})
}

func (s *Server) handleWalletHistory(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	orders, err := mobileDB(s.db).ListOrdersByUser(r.Context(), uid, 50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": orders, "count": len(orders)})
}
