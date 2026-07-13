package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
	"payment-gateway/internal/httpclient"
	"payment-gateway/internal/security"
)

type BuySendWorker struct {
	bus    *EventBus
	db     *database.DB
	cfg    *config.Config
	client *http.Client
}

func NewBuySendWorker(bus *EventBus, db *database.DB, cfg *config.Config) *BuySendWorker {
	return &BuySendWorker{
		bus:    bus,
		db:     db,
		cfg:    cfg,
		client: httpclient.Default(),
	}
}

func (bw *BuySendWorker) Start(ctx context.Context) {
	buyChan := bw.bus.Subscribe("buy.paid")
	slog.Info("BuySendWorker escutando eventos 'buy.paid'")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Desligando BuySendWorker")
			return
		case <-ticker.C:
			bw.recoverPendingBuys(ctx)
		case event, ok := <-buyChan:
			if !ok {
				return
			}
			bw.dispatch(event)
		}
	}
}

func (bw *BuySendWorker) dispatch(event Event) {
	go func() {
		bw.processBuyOnchainSend(event)
	}()
}

func (bw *BuySendWorker) recoverPendingBuys(ctx context.Context) {
	scanCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	buys, err := bw.db.ListPendingBuys(scanCtx)
	if err != nil {
		slog.Error("Erro ao varrer BUYs pendentes para recovery", "error", err)
		return
	}
	for _, buy := range buys {
		bw.dispatch(Event{Type: "buy.recovery", OrderID: buy.ID})
	}
	if len(buys) > 0 {
		slog.Info("Recovery BUY varreu ordens pagas pendentes", "count", len(buys))
	}
}

func (bw *BuySendWorker) processBuyOnchainSend(event Event) {
	start := time.Now()
	orderID := event.OrderID
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// ── Atomic DB claim: replaces in-memory active map ────────────────────────
	// UPDATE ... WHERE status IN ('pago_fiat','pago_pix') RETURNING id ensures
	// only one worker (across goroutines AND replicas) processes each order.
	// The in-memory sync.Map is insufficient for multi-replica deployments.
	claimCtx, claimCancel := context.WithTimeout(ctx, 5*time.Second)
	claimed, err := bw.db.ClaimBuyOrderForSend(claimCtx, orderID)
	claimCancel()
	if err != nil {
		slog.Error("Erro ao tentar claim de buy order", "buy_order_id", orderID, "error", err)
		return
	}
	if !claimed {
		slog.Debug("BuySendWorker: buy order já processada por outro worker", "buy_order_id", orderID)
		return
	}

	buy, err := bw.db.GetBuyOrder(ctx, orderID)
	if err != nil {
		slog.Error("Erro ao buscar buy order", "buy_order_id", orderID, "error", err)
		return
	}
	if buy == nil {
		return
	}

	// Validação do signer
	if bw.cfg.SignerUrl == "" || bw.cfg.SignerHmacSecret == "" {
		if bw.cfg.AllowSimulations && !bw.cfg.IsProduction() {
			txHash := "buy-sim-" + orderID
			if err := bw.db.UpdateBuyOrderStatus(ctx, orderID, "enviado", map[string]any{"txHashOut": txHash}); err != nil {
				slog.Error("Erro ao persistir envio BUY simulado", "buy_order_id", orderID, "error", err)
				return
			}
			bw.bus.Publish(Event{Type: "buy.sent", OrderID: orderID, Payload: map[string]any{"txHash": txHash}})
			slog.Warn("Signer nao configurado; envio BUY simulado", "buy_order_id", orderID, "tx_hash", txHash)
			return
		}
		_ = bw.db.UpdateBuyOrderStatus(ctx, orderID, "erro", map[string]any{"error": "SIGNER_URL ou SIGNER_HMAC_SECRET nao configurado"})
		slog.Error("Envio BUY bloqueado: signer ausente", "buy_order_id", orderID)
		return
	}

	// Prepara payload
	network := strings.ToUpper(strings.TrimSpace(bw.cfg.SignerNetwork))
	if network == "" || network == "EVM" || network == "BINANCE" || network == "BEP20" {
		network = "BSC"
	}

	payload := map[string]any{
		"to":             buy.DestAddress,
		"amount":         fmt.Sprintf("%.8f", buy.CryptoAmount),
		"tokenContract":  bw.cfg.BscUsdtContract,
		"network":        network,
		"idempotencyKey": "buy-" + buy.ID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Erro ao serializar payload", "buy_order_id", orderID, "error", err)
		_ = bw.db.UpdateBuyOrderStatus(ctx, orderID, "erro", map[string]any{"error": "Erro ao serializar payload"})
		return
	}

	// Envia para signer
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bw.cfg.SignerUrl+"/hd/transfer", bytes.NewReader(body))
	if err != nil {
		slog.Error("Erro ao montar request para signer BUY", "buy_order_id", orderID, "error", err)
		_ = bw.db.UpdateBuyOrderStatus(ctx, orderID, "erro", map[string]any{"error": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	security.SignRawBodyHeaders(req, bw.cfg.SignerHmacSecret, body)

	resp, err := bw.client.Do(req)
	if err != nil {
		_ = bw.db.UpdateBuyOrderStatus(ctx, orderID, "pendente_confirmacao", map[string]any{"error": err.Error()})
		slog.Error("Signer BUY ambiguo; ordem marcada como pendente_confirmacao", "buy_order_id", orderID, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errMsg := fmt.Sprintf("signer status %d", resp.StatusCode)
		_ = bw.db.UpdateBuyOrderStatus(ctx, orderID, "pendente_confirmacao", map[string]any{"error": errMsg})
		slog.Error("Signer BUY retornou status ambiguo; ordem marcada como pendente_confirmacao", "buy_order_id", orderID, "status", resp.StatusCode)
		return
	}

	// Processa resposta
	var signed struct {
		TxHash string `json:"txHash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&signed); err != nil {
		slog.Error("Erro ao decodificar resposta do signer", "buy_order_id", orderID, "error", err)
		signed.TxHash = "signer-accepted-" + orderID // fallback
	}

	if signed.TxHash == "" {
		signed.TxHash = "signer-accepted-" + orderID
	}

	if strings.HasPrefix(signed.TxHash, "signer-accepted-") {
		if err := bw.db.UpdateBuyOrderStatus(ctx, orderID, "pendente_confirmacao", map[string]any{"txHashOut": signed.TxHash}); err != nil {
			slog.Error("Erro ao atualizar BUY pendente_confirmacao", "buy_order_id", orderID, "error", err)
			return
		}
		bw.bus.Publish(Event{Type: "buy.pending_confirmation", OrderID: orderID, Payload: map[string]any{"txHash": signed.TxHash}})
		slog.Warn("Envio cripto BUY aceito sem txHash; aguardando confirmacao manual/signer", "buy_order_id", orderID, "duration_ms", time.Since(start).Milliseconds())
		return
	}

	if err := bw.db.UpdateBuyOrderStatus(ctx, orderID, "enviado", map[string]any{"txHashOut": signed.TxHash}); err != nil {
		slog.Error("Erro ao atualizar BUY enviado", "buy_order_id", orderID, "error", err)
		return
	}
	bw.bus.Publish(Event{Type: "buy.sent", OrderID: orderID, Payload: map[string]any{"txHash": signed.TxHash}})
	slog.Info("Envio cripto BUY processado", "buy_order_id", orderID, "duration_ms", time.Since(start).Milliseconds())
}
