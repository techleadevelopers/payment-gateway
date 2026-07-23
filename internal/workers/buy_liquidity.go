package workers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
	"payment-gateway/internal/liquidity"
)

func newBuyLiquidityRouter(cfg *config.Config, client *http.Client) *liquidity.Router {
	if cfg == nil || !cfg.LiquidityRouterEnabled {
		return nil
	}
	var providers []liquidity.Provider
	for _, entry := range splitCSV(cfg.LiquidityProviderURLs) {
		name, baseURL := parseProviderEntry(entry)
		if baseURL == "" {
			continue
		}
		providers = append(providers, &liquidity.HTTPProvider{
			ProviderName: name,
			BaseURL:      baseURL,
			APIKey:       cfg.LiquidityProviderAPIKey,
			Client:       client,
		})
	}
	return liquidity.NewRouter(providers...)
}

func (bw *BuySendWorker) tryLiquidityExecution(ctx context.Context, buy *database.BuyOrder) bool {
	if bw == nil || bw.cfg == nil || !bw.cfg.LiquidityRouterEnabled || bw.router == nil || buy == nil {
		return false
	}
	if !containsCSVFold(bw.cfg.LiquidityAllowedAssets, buy.Asset) || !containsCSVFold(bw.cfg.LiquidityAllowedNetworks, buy.Network) {
		return false
	}
	timeout := time.Duration(bw.cfg.LiquidityQuoteTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2500 * time.Millisecond
	}
	routeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req := liquidity.Request{
		OrderID:         buy.ID,
		Asset:           buy.Asset,
		Network:         buy.Network,
		FiatCurrency:    buy.FiatCurrency,
		AmountBRL:       buy.PayoutBRL,
		CryptoAmount:    buy.CryptoAmount,
		QuoteLockedRate: buy.RateLocked,
		DestAddress:     buy.DestAddress,
		CreatedAt:       buy.CreatedAt,
	}
	best, quotes, exec, err := bw.router.ExecuteBest(routeCtx, req)
	if len(quotes) > 0 {
		quoteRecords := make([]database.LiquidityQuoteRecord, 0, len(quotes))
		for _, quote := range quotes {
			quoteRecords = append(quoteRecords, liquidityQuoteRecord(quote, quote.Provider == best.Provider))
		}
		quoteIDs, recordErr := bw.db.RecordLiquidityQuotes(ctx, buy.ID, quoteRecords)
		if recordErr != nil {
			slog.Warn("liquidity: falha ao gravar quotes", "buy_order_id", buy.ID, "error", recordErr)
		}
		if best.Provider != "" {
			execQuoteID := quoteIDs[best.Provider]
			_ = bw.db.RecordLiquidityExecution(ctx, buy.ID, liquidityExecutionRecord(execQuoteID, best.Provider, "attempted", exec, err))
		}
	}
	if err != nil {
		_ = bw.db.AddBuyEvent(ctx, buy.ID, "buy.liquidity.fallback", map[string]any{
			"error": err.Error(),
		})
		if !errors.Is(err, liquidity.ErrNoProviderQuote) && !errors.Is(err, liquidity.ErrNoExecutable) {
			slog.Warn("liquidity: execucao falhou; fallback para hot wallet", "buy_order_id", buy.ID, "error", err)
		}
		return false
	}

	status := strings.ToLower(strings.TrimSpace(exec.Status))
	if status == "" {
		status = "submitted"
	}
	switch status {
	case "sent", "enviado", "delivered", "confirmed", "settled":
		txHash := strings.TrimSpace(exec.TxHash)
		if txHash == "" {
			txHash = strings.TrimSpace(exec.ExternalOrderID)
		}
		if txHash == "" {
			txHash = "liquidity-accepted-" + buy.ID
		}
		if err := bw.db.UpdateBuyOrderStatus(ctx, buy.ID, "enviado", map[string]any{"txHashOut": txHash, "provider": exec.Provider}); err != nil {
			slog.Error("liquidity: falha ao atualizar BUY enviado", "buy_order_id", buy.ID, "error", err)
			return false
		}
		bw.bus.Publish(Event{Type: "buy.sent", OrderID: buy.ID, Payload: map[string]any{"txHash": txHash, "provider": exec.Provider}})
		return true
	default:
		txHash := strings.TrimSpace(exec.TxHash)
		if txHash == "" {
			txHash = "liquidity-accepted-" + buy.ID
		}
		if err := bw.db.UpdateBuyOrderStatus(ctx, buy.ID, "pendente_confirmacao", map[string]any{"txHashOut": txHash, "provider": exec.Provider, "externalOrderId": exec.ExternalOrderID}); err != nil {
			slog.Error("liquidity: falha ao atualizar BUY pendente", "buy_order_id", buy.ID, "error", err)
			return false
		}
		bw.bus.Publish(Event{Type: "buy.pending_confirmation", OrderID: buy.ID, Payload: map[string]any{"txHash": txHash, "provider": exec.Provider}})
		return true
	}
}

func liquidityQuoteRecord(quote liquidity.Quote, selected bool) database.LiquidityQuoteRecord {
	return database.LiquidityQuoteRecord{
		Provider:           quote.Provider,
		ProviderType:       quote.ProviderType,
		ExternalQuoteID:    quote.ExternalQuoteID,
		Asset:              quote.Asset,
		Network:            quote.Network,
		FiatCostBRL:        quote.FiatCostBRL,
		ProviderFeeBRL:     quote.ProviderFeeBRL,
		NetworkFeeBRL:      quote.NetworkFeeBRL,
		SpreadBRL:          quote.SpreadBRL,
		TotalCostBRL:       quote.TotalCostBRL,
		CryptoAmount:       quote.CryptoAmount,
		DeliverySLASeconds: quote.DeliverySLASeconds,
		ReliabilityBps:     quote.ReliabilityBps,
		DirectDelivery:     quote.DirectDelivery,
		Selected:           selected,
		ExpiresAt:          quote.ExpiresAt,
		Payload:            quote,
	}
}

func liquidityExecutionRecord(quoteID, provider, status string, exec liquidity.Execution, execErr error) database.LiquidityExecutionRecord {
	errMsg := ""
	if execErr != nil {
		errMsg = execErr.Error()
		status = "failed"
	}
	return database.LiquidityExecutionRecord{
		QuoteID:         quoteID,
		Provider:        firstNonEmptyWorker(exec.Provider, provider),
		Status:          status,
		ExternalOrderID: exec.ExternalOrderID,
		TxHash:          exec.TxHash,
		Error:           errMsg,
		Payload:         exec,
	}
}

func parseProviderEntry(entry string) (string, string) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return "", ""
	}
	if parts := strings.SplitN(entry, "=", 2); len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "provider", entry
}

func containsCSVFold(raw, value string) bool {
	items := splitCSV(raw)
	if len(items) == 0 {
		return true
	}
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, item := range items {
		if strings.ToUpper(strings.TrimSpace(item)) == value {
			return true
		}
	}
	return false
}

func splitCSV(raw string) []string {
	var out []string
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func firstNonEmptyWorker(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
