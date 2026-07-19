package mobile

import (
	"net/http"
	"strings"
	"time"

	"payment-gateway/internal/database"
	"payment-gateway/internal/nfc"
)

func (s *Server) handleNFCCard(w http.ResponseWriter, r *http.Request) {
	if !s.nfcReady(w) {
		return
	}
	user, ok := s.nfcUserWithWallet(w, r)
	if !ok {
		return
	}
	network := "BSC"
	bal, err := s.db.GetNFCBalance(r.Context(), *user.WalletAddress, network)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"card": map[string]any{
			"type":           "chainfx_tap_usdt",
			"display_name":   "ChainFX Tap",
			"wallet_address": *user.WalletAddress,
			"network":        network,
			"asset":          "USDT",
			"aid":            nfc.ChainFXAIDHex,
			"hce":            true,
			"scheme":         "chainfx_own_closed_loop",
			"card_network":   "none",
			"fiat_settlement": map[string]any{
				"rail":     "efi_pix",
				"provider": "efi",
				"mode":     "chainfx_terminal_to_chainfx_backend",
			},
			"crypto_debit": map[string]any{
				"asset":  "USDT",
				"source": "nfc_internal_usdt_ledger",
			},
		},
		"balance": nfcBalanceForMobile(bal),
	})
}

func (s *Server) handleNFCProvision(w http.ResponseWriter, r *http.Request) {
	if !s.nfcReady(w) {
		return
	}
	user, ok := s.nfcUserWithWallet(w, r)
	if !ok {
		return
	}
	var req struct {
		DeviceID   string `json:"device_id"`
		Network    string `json:"network"`
		TTLSeconds int    `json:"ttl_seconds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON payload"})
		return
	}
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.Network = strings.ToUpper(strings.TrimSpace(req.Network))
	if req.Network == "" {
		req.Network = "BSC"
	}
	if req.DeviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "device_id obrigatorio"})
		return
	}
	if req.Network != "BSC" && req.Network != "POLYGON" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "network deve ser BSC ou POLYGON"})
		return
	}
	ttl := time.Duration(s.cfg.NFCTokenTTLSeconds) * time.Second
	if req.TTLSeconds > 0 && req.TTLSeconds <= 300 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
	}
	now := time.Now().UTC()
	token, claims, err := nfc.IssueToken(s.cfg.NFCTokenSecret, *user.WalletAddress, req.DeviceID, req.Network, ttl, now)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	if err := s.db.StoreNFCToken(r.Context(), database.NFCTokenInput{
		TokenID:   claims.TokenID,
		TokenHash: nfc.TokenHash(token),
		Wallet:    claims.Wallet,
		DeviceID:  claims.DeviceID,
		Network:   claims.Network,
		ExpiresAt: time.Unix(claims.ExpiresAtUnix, 0),
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":      token,
		"token_id":   claims.TokenID,
		"expires_at": time.Unix(claims.ExpiresAtUnix, 0).UTC(),
		"aid":        nfc.ChainFXAIDHex,
		"network":    claims.Network,
		"apdu": map[string]any{
			"response_template": "70",
			"token_tag":         "DF01",
			"version_tag":       "DF02",
		},
	})
}

func (s *Server) nfcReady(w http.ResponseWriter) bool {
	if s == nil || s.cfg == nil || !s.cfg.NFCEnabled {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "NFC desabilitado"})
		return false
	}
	if strings.TrimSpace(s.cfg.NFCTokenSecret) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "NFC_TOKEN_SECRET nao configurado"})
		return false
	}
	return true
}

func (s *Server) nfcUserWithWallet(w http.ResponseWriter, r *http.Request) (*databaseUserView, bool) {
	uid := userIDFromCtx(r)
	user, err := mobileDB(s.db).GetUserByID(r.Context(), uid)
	if err != nil || user == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "usuario nao encontrado"})
		return nil, false
	}
	if user.WalletAddress == nil || strings.TrimSpace(*user.WalletAddress) == "" {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "wallet do usuario nao registrada"})
		return nil, false
	}
	return &databaseUserView{WalletAddress: user.WalletAddress}, true
}

type databaseUserView struct {
	WalletAddress *string
}

func nfcBalanceForMobile(b *database.NFCBalance) map[string]any {
	return map[string]any{
		"available_usdt": float64(b.AvailableMicro) / 1_000_000,
		"locked_usdt":    float64(b.LockedMicro) / 1_000_000,
		"network":        b.Network,
		"asset":          b.Asset,
	}
}
