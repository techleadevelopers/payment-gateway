package mobile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// handleMobileBuy — POST /api/mobile/order/buy
// Delegates to the existing POST /api/buy handler internally.
func (s *Server) handleMobileBuy(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	var req struct {
		AmountBRL     float64 `json:"amount_brl"`
		Asset         string  `json:"asset"`
		DestAddress   string  `json:"dest_address"`
		PaymentMethod string  `json:"payment_method"` // "pix" | "card"
		CustomerName  string  `json:"customer_name"`
		CustomerEmail string  `json:"customer_email"`
		CustomerCPF   string  `json:"customer_cpf"`
	}
	if err := decodeJSON(r, &req); err != nil || req.AmountBRL <= 0 || req.DestAddress == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "amount_brl e dest_address obrigatórios"})
		return
	}
	if req.Asset == "" {
		req.Asset = "USDT"
	}
	if req.PaymentMethod == "" {
		req.PaymentMethod = "pix"
	}

	// Forward to existing /api/buy
	payload := map[string]any{
		"amountBRL":     req.AmountBRL,
		"asset":         req.Asset,
		"address":       req.DestAddress,
		"paymentMethod": req.PaymentMethod,
		"customer": map[string]any{
			"name":  req.CustomerName,
			"email": req.CustomerEmail,
			"cpf":   req.CustomerCPF,
		},
	}
	resp, err := forwardToInternal(r, "POST", s.internalBase(r)+"/api/buy", payload, s.internalAPIKey())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "erro ao criar ordem: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Tag order with user_id if we got an id back
	var result map[string]any
	if json.Unmarshal(body, &result) == nil {
		if id, ok := result["id"].(string); ok && id != "" {
			_ = mobileDB(s.db).TagBuyOrderUser(r.Context(), id, uid)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// handleMobileSell — POST /api/mobile/order/sell
// Delegates to existing POST /api/order handler.
func (s *Server) handleMobileSell(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	var req struct {
		AmountUSDT float64 `json:"amount_usdt"`
		PixKey     string  `json:"pix_key"`
		PixCpf     string  `json:"pix_cpf"`
		PixPhone   string  `json:"pix_phone"`
		Asset      string  `json:"asset"`
	}
	if err := decodeJSON(r, &req); err != nil || req.AmountUSDT <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "amount_usdt obrigatório"})
		return
	}
	pixKey := req.PixKey
	if pixKey == "" && req.PixPhone != "" {
		pixKey = req.PixPhone
	}
	if pixKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "pix_key ou pix_phone obrigatório"})
		return
	}
	payload := map[string]any{
		"amountUSDT": req.AmountUSDT,
		"pixPhone":   pixKey,
		"pixCpf":     req.PixCpf,
		"asset":      firstNonEmptyStr(req.Asset, "USDT"),
	}
	resp, err := forwardToInternal(r, "POST", s.internalBase(r)+"/api/order", payload, s.internalAPIKey())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if json.Unmarshal(body, &result) == nil {
		if id, ok := result["id"].(string); ok && id != "" {
			_ = mobileDB(s.db).TagOrderUser(r.Context(), id, uid)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// handleMobileSwap — POST /api/mobile/order/swap
// Stub: swap = sell → buy. Returns instructions for two-leg swap.
func (s *Server) handleMobileSwap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromAsset string  `json:"from_asset"`
		ToAsset   string  `json:"to_asset"`
		Amount    float64 `json:"amount"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "from_asset, to_asset e amount obrigatórios"})
		return
	}
	price := s.PriceCache().GetCurrentPrice()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"type":       "swap",
		"from_asset": req.FromAsset,
		"to_asset":   req.ToAsset,
		"amount":     req.Amount,
		"rate":       price,
		"status":     "quote_only",
		"hint":       "Swap direto em andamento. Use sell + buy para executar agora.",
	})
}

// handleMobileGetOrder — GET /api/mobile/order/{id}
func (s *Server) handleMobileGetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	resp, err := forwardToInternal(r, "GET", s.internalBase(r)+"/api/order/"+id, nil, s.internalAPIKey())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// handleMobileListOrders — GET /api/mobile/orders
func (s *Server) handleMobileListOrders(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	orders, err := mobileDB(s.db).ListOrdersByUser(r.Context(), uid, 20)
	if err != nil {
		slog.Error("erro interno", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "erro interno"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": orders, "count": len(orders)})
}

// handleMobileCancelOrder — POST /api/mobile/order/cancel
func (s *Server) handleMobileCancelOrder(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromCtx(r)
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.OrderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "order_id obrigatório"})
		return
	}
	if err := mobileDB(s.db).CancelOrder(r.Context(), req.OrderID, uid); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (s *Server) internalBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}
	host := r.Host
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}
	if host == "" {
		host = fmt.Sprintf("localhost:%s", s.cfg.Port)
	}
	return scheme + "://" + host
}

func (s *Server) internalAPIKey() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	if key := strings.TrimSpace(s.cfg.ChainFXLiveSecretKeys); key != "" {
		return key
	}
	return strings.TrimSpace(s.cfg.ChainFXTestSecretKeys)
}

func forwardToInternal(r *http.Request, method, url string, payload any, apiKey string) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		first := strings.Split(apiKey, ",")[0]
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(first))
	}
	return http.DefaultClient.Do(req)
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
