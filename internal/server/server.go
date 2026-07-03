package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
	"payment-gateway/internal/email"
	"payment-gateway/internal/models"
	"payment-gateway/internal/tron"
	"payment-gateway/internal/workers"
)

type Server struct {
	cfg     *config.Config
	db      *database.DB
	workers *workers.WorkerManager
	tron    *tron.Client
	email   *email.Service
	limiter *rateLimiter
}

func New(cfg *config.Config, db *database.DB, workerMgr *workers.WorkerManager, mailer *email.Service) *Server {
	return &Server{
		cfg:     cfg,
		db:      db,
		workers: workerMgr,
		tron:    tron.NewClient(cfg),
		email:   mailer,
		limiter: newRateLimiter(cfg.OrderRateLimitWindowMs, cfg.OrderRateLimitMax),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/price", s.handlePrice)
	mux.HandleFunc("POST /api/order", s.handleCreateOrder)
	mux.HandleFunc("GET /api/order/{id}", s.handleGetOrder)
	mux.HandleFunc("GET /api/order/{id}/stream", s.handleOrderStream)
	mux.HandleFunc("POST /api/order/{id}/deposit", s.handleDeposit)
	mux.HandleFunc("POST /api/order/{id}/payout", s.handlePayout)
	mux.HandleFunc("POST /api/pix/webhook", s.handlePixWebhook)
	mux.HandleFunc("POST /internal/sweep", s.handleInternalSweep)
	mux.HandleFunc("POST /internal/email/test", s.handleEmailTest)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, map[string]any{"ok": true}) })
	mux.HandleFunc("GET /readyz", s.handleReady)
	return securityHeaders(cors(s.cfg, logRequests(mux)))
}

func (s *Server) handlePrice(w http.ResponseWriter, r *http.Request) {
	price := s.workers.PriceWorker.GetCurrentPrice()
	if price <= 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "preço ainda não carregado"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"brl": price})
}

func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.Allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "limite de criação de ordens excedido"})
		return
	}
	var req struct {
		AmountBRL float64 `json:"amountBRL"`
		Address   string  `json:"address"`
		Network   string  `json:"network"`
		Asset     string  `json:"asset"`
		PixCpf    string  `json:"pixCpf"`
		PixPhone  string  `json:"pixPhone"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "JSON inválido"})
		return
	}
	if req.PixCpf == "" && req.PixPhone == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "pixCpf ou pixPhone é obrigatório"})
		return
	}
	if req.AmountBRL < s.cfg.OrderMinBrl || req.AmountBRL > s.cfg.OrderMaxBrl {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("valor fora dos limites (%.2f - %.2f BRL)", s.cfg.OrderMinBrl, s.cfg.OrderMaxBrl)})
		return
	}
	network := strings.ToUpper(defaultString(req.Network, "TRON"))
	asset := strings.ToUpper(defaultString(req.Asset, "USDT"))
	if network != "TRON" || asset != "USDT" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "somente pedidos TRON/USDT são suportados"})
		return
	}
	ctx := r.Context()
	stats, err := s.db.StatsPixLast24h(ctx, req.PixCpf, req.PixPhone)
	if err != nil {
		writeError(w, err)
		return
	}
	if stats.Count >= s.cfg.PixMaxOrdersPer24h || stats.Total+req.AmountBRL > s.cfg.PixMaxBrlPer24h {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "limite diário por chave PIX excedido"})
		return
	}

	var idx *int
	depositAddress := strings.TrimSpace(req.Address)
	if depositAddress == "" {
		next, err := s.db.NextDerivationIndex(ctx)
		if err != nil {
			writeError(w, err)
			return
		}
		addr, err := s.tron.DeriveAddress(next)
		if err != nil {
			writeError(w, err)
			return
		}
		idx = &next
		depositAddress = addr
	} else if !tron.IsAddress(depositAddress) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "endereço TRON inválido"})
		return
	}

	rate := s.workers.PriceWorker.GetCurrentPrice()
	if rate <= 0 {
		rate = 5.0
	}
	fee := math.Max(s.cfg.FeeMinBrl, req.AmountBRL*(float64(s.cfg.FeeBps)/10000))
	payout := req.AmountBRL - fee
	if payout <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "valor insuficiente após taxa"})
		return
	}
	amountUSDT := payout / rate
	order, err := s.db.CreateOrder(ctx, database.OrderInput{
		Status:            string(models.StatusAguardandoDeposito),
		AmountBRL:         req.AmountBRL,
		AmountUSDT:        amountUSDT,
		FeeBRL:            fee,
		PayoutBRL:         payout,
		Address:           depositAddress,
		Asset:             asset,
		Network:           network,
		RateLocked:        rate,
		RateLockExpiresAt: time.Now().Add(time.Duration(s.cfg.RateLockSec) * time.Second),
		PixCpf:            req.PixCpf,
		PixPhone:          req.PixPhone,
		DerivationIndex:   idx,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.db.AddEvent(ctx, order.ID, "order.meta", map[string]any{"ip": clientIP(r), "userAgent": r.UserAgent(), "pixCpf": req.PixCpf, "pixPhone": req.PixPhone})
	s.workers.Bus.Publish(workers.Event{Type: "order.created", OrderID: order.ID, Payload: map[string]any{"amountBRL": req.AmountBRL}})
	s.email.NotifyOps("Swappy: nova ordem criada", fmt.Sprintf("Ordem %s criada para %.2f BRL. Endereço: %s", order.ID, req.AmountBRL, depositAddress))
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": order.ID, "orderId": order.ID, "status": order.Status, "address": depositAddress, "depositAddress": depositAddress,
		"amountBRL": req.AmountBRL, "amountUSDT": amountUSDT, "btcAmount": amountUSDT, "feeBRL": fee, "payoutBRL": payout,
		"rate": rate, "network": network,
	})
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	order, err := s.db.GetOrder(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	if order == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "ordem não encontrada"})
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (s *Server) handleOrderStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var last string
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			order, _ := s.db.GetOrder(r.Context(), r.PathValue("id"))
			if order == nil {
				continue
			}
			status := string(order.Status)
			if status != last {
				last = status
				raw, _ := json.Marshal(map[string]any{"status": status, "txHash": order.TxHash})
				_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
	}
}

func (s *Server) handleDeposit(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if !validHMAC(s.cfg.TronHmacSecret, raw, r.Header.Get("x-internal-hmac")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "assinatura inválida"})
		return
	}
	var req struct {
		TxHash string  `json:"txHash"`
		Amount float64 `json:"amount"`
	}
	if err := json.Unmarshal(raw, &req); err != nil || req.TxHash == "" || req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "payload inválido"})
		return
	}
	id := r.PathValue("id")
	if idem := r.Header.Get("x-idempotency-key"); idem != "" {
		exists, _ := s.db.HasEvent(r.Context(), id, "idempotency", "key", idem)
		if exists {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "duplicate": true})
			return
		}
		_ = s.db.AddEvent(r.Context(), id, "idempotency", map[string]any{"key": idem, "endpoint": "deposit"})
	}
	order, err := s.db.GetOrder(r.Context(), id)
	if err != nil || order == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "ordem não encontrada"})
		return
	}
	if order.Status != models.StatusAguardandoDeposito {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "status atual não permite depósito"})
		return
	}
	if err := s.db.UpdateOrderStatus(r.Context(), id, "pago", map[string]any{"depositTx": req.TxHash, "depositAmount": req.Amount}); err != nil {
		writeError(w, err)
		return
	}
	s.workers.Bus.Publish(workers.Event{Type: "onchain.detected", OrderID: id, Payload: map[string]any{"tx_hash": req.TxHash, "amount_usdt": req.Amount}})
	s.workers.Bus.Publish(workers.Event{Type: "payout.requested", OrderID: id})
	s.email.NotifyOps("Swappy: depósito detectado", fmt.Sprintf("Ordem %s recebeu depósito %s no valor %.8f USDT.", id, req.TxHash, req.Amount))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePayout(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if !validHMAC(s.cfg.TronHmacSecret, raw, r.Header.Get("x-internal-hmac")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "assinatura inválida"})
		return
	}
	var req struct {
		ProviderID string `json:"providerId"`
		Status     string `json:"status"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(raw, &req); err != nil || req.ProviderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "payload inválido"})
		return
	}
	status := "erro"
	extra := map[string]any{"error": req.Error}
	if strings.HasPrefix(strings.ToLower(req.Status), "conclu") {
		status = "concluida"
		extra = map[string]any{"txHash": req.ProviderID}
	}
	if err := s.db.UpdateOrderStatus(r.Context(), r.PathValue("id"), status, extra); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePixWebhook(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	secret := defaultString(s.cfg.PixWebhookSecret, s.cfg.WebhookSecret)
	if !validHMAC(secret, raw, r.Header.Get("x-pagbank-signature")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "assinatura inválida"})
		return
	}
	var req struct {
		OrderID    string `json:"orderId"`
		Status     string `json:"status"`
		ProviderID string `json:"providerId"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(raw, &req); err != nil || req.OrderID == "" || req.ProviderID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "payload inválido"})
		return
	}
	status := "erro"
	extra := map[string]any{"error": req.Error}
	if strings.HasPrefix(strings.ToLower(req.Status), "conclu") {
		status = "concluida"
		extra = map[string]any{"txHash": req.ProviderID}
	}
	if err := s.db.UpdateOrderStatus(r.Context(), req.OrderID, status, extra); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleInternalSweep(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if !validHMAC(s.cfg.TronHmacSecret, raw, r.Header.Get("x-internal-hmac")) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "assinatura inválida"})
		return
	}
	var req struct {
		ChildIndex int     `json:"childIndex"`
		ToAddr     string  `json:"toAddr"`
		Amount     float64 `json:"amount"`
	}
	if err := json.Unmarshal(raw, &req); err != nil || req.Amount <= 0 || req.ToAddr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "payload inválido"})
		return
	}
	from, err := s.tron.DeriveAddress(req.ChildIndex)
	if err != nil {
		writeError(w, err)
		return
	}
	sweep, err := s.db.CreateSweep(r.Context(), req.ChildIndex, from, req.ToAddr, req.Amount, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "sweepId": sweep.ID})
}

func (s *Server) handleEmailTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "JSON inválido"})
		return
	}
	if req.Subject == "" {
		req.Subject = "Swappy Financial - teste SMTP"
	}
	if req.Body == "" {
		req.Body = "Serviço de email operacional ativo."
	}
	if err := s.email.Send(email.Message{To: req.To, Subject: req.Subject, Body: req.Body}); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "db": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "db": true, "tron": s.cfg.TronFullNodeURL != "" || s.cfg.TronFullNodeUrl != ""})
}

func decodeJSON(r *http.Request, dest any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, err error) {
	slog.Error("Erro HTTP", "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "erro interno"})
}

func validHMAC(secret string, raw []byte, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

func cors(cfg *config.Config, next http.Handler) http.Handler {
	allowed := strings.Split(cfg.AllowedOrigins, ",")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		for _, item := range allowed {
			item = strings.TrimSpace(item)
			if item == "*" || item == origin || (origin == "" && item != "") {
				w.Header().Set("Access-Control-Allow-Origin", defaultString(origin, item))
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, x-internal-hmac, x-idempotency-key, x-pagbank-signature")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				break
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		slog.Info("http_request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	return r.RemoteAddr
}

type rateLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	max      int
	counters map[string]rateBucket
}

type rateBucket struct {
	ResetAt time.Time
	Count   int
}

func newRateLimiter(windowMs, max int) *rateLimiter {
	if windowMs <= 0 {
		windowMs = 60000
	}
	if max <= 0 {
		max = 20
	}
	return &rateLimiter{window: time.Duration(windowMs) * time.Millisecond, max: max, counters: make(map[string]rateBucket)}
}

func (l *rateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.counters[key]
	if b.ResetAt.IsZero() || now.After(b.ResetAt) {
		b = rateBucket{ResetAt: now.Add(l.window)}
	}
	b.Count++
	l.counters[key] = b
	return b.Count <= l.max
}
