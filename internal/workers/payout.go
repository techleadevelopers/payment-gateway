package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
)

type PayoutWorker struct {
	bus    *EventBus
	db     *database.DB
	cfg    *config.Config
	client *http.Client
}

func NewPayoutWorker(bus *EventBus, db *database.DB, cfg *config.Config) *PayoutWorker {
	return &PayoutWorker{
		bus:    bus,
		db:     db,
		cfg:    cfg,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Start assina a fila de eventos e processa cada requisição de PIX concorrentemente
func (pw *PayoutWorker) Start(ctx context.Context) {
	// Se inscreve na fila interna (Substitui o subscribe do Node)
	payoutChan := pw.bus.Subscribe("payout.requested")
	slog.Info("PayoutWorker escutando eventos 'payout.requested'...")

	for {
		select {
		case <-ctx.Done():
			slog.Info("Desligando PayoutWorker de forma limpa...")
			return
		case event, ok := <-payoutChan:
			if !ok {
				return
			}

			// Dispara uma Goroutine por pagamento para que um PIX lento não trave os outros
			go pw.processPayout(event)
		}
	}
}

func (pw *PayoutWorker) processPayout(event Event) {
	start := time.Now()
	orderID := event.OrderID

	slog.Info("Processando solicitação de Payout PIX", "order_id", orderID)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	order, err := pw.db.GetOrder(ctx, orderID)
	if err != nil {
		slog.Error("Erro ao buscar ordem para payout", "order_id", orderID, "error", err)
		return
	}
	if order == nil || string(order.Status) != "pago" {
		return
	}

	// Simulação idêntica à lógica do seu Node.js quando falta o Token do PagBank
	if pw.cfg.PagSeguroApiToken == "" {
		slog.Warn("PagBank token não configurado, executando simulação de PIX", "order_id", orderID)
		txHash := fmt.Sprintf("pix-sim-%s", orderID)
		if err := pw.db.UpdateOrderStatus(ctx, orderID, "concluida", map[string]interface{}{"txHash": txHash}); err != nil {
			slog.Error("Erro ao persistir payout simulado", "order_id", orderID, "error", err)
			return
		}

		// Publica o encerramento do fluxo no barramento interno
		pw.bus.Publish(Event{
			Type:    "payout.settled",
			OrderID: orderID,
			Payload: map[string]interface{}{
				"status":      "concluida",
				"tx_hash_pix": txHash,
			},
		})

		slog.Info("Payout simulado concluído com sucesso",
			"order_id", orderID,
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return
	}

	payload := map[string]interface{}{
		"txId": order.ID,
		"value": map[string]interface{}{
			"currency": "BRL",
			"amount":   order.PayoutBRL,
		},
		"payer": map[string]interface{}{
			"name":  "Cliente",
			"taxId": order.PixCpf,
		},
		"key":         firstNonEmpty(order.PixPhone, order.PixCpf),
		"description": "Off-ramp USDT->PIX",
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pw.cfg.PagSeguroApiBaseUrl+"/instant-payments", bytes.NewReader(raw))
	if err != nil {
		slog.Error("Erro ao montar request PagBank", "order_id", orderID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+pw.cfg.PagSeguroApiToken)
	resp, err := pw.client.Do(req)
	if err != nil {
		_ = pw.db.UpdateOrderStatus(ctx, orderID, "erro", map[string]interface{}{"error": err.Error()})
		slog.Error("Erro no payout PagBank", "order_id", orderID, "error", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = pw.db.UpdateOrderStatus(ctx, orderID, "erro", map[string]interface{}{"error": fmt.Sprintf("PagBank status %d", resp.StatusCode)})
		slog.Error("PagBank rejeitou payout", "order_id", orderID, "status", resp.StatusCode)
		return
	}
	providerID := fmt.Sprintf("pagbank-%s", orderID)
	if err := pw.db.UpdateOrderStatus(ctx, orderID, "concluida", map[string]interface{}{"txHash": providerID}); err != nil {
		slog.Error("Erro ao persistir payout PagBank", "order_id", orderID, "error", err)
		return
	}
	pw.bus.Publish(Event{Type: "payout.settled", OrderID: orderID, Payload: map[string]interface{}{"status": "concluida", "providerId": providerID}})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "chave@pix.com"
}
