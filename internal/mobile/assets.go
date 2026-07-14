package mobile

// assets.go — Phase 5: Multi-Asset endpoints (mobile-only)
//
//	GET  /api/mobile/assets            — list active assets
//	GET  /api/mobile/assets/{symbol}   — single asset config
//	GET  /api/mobile/assets/{symbol}/rate — live price in BRL/USD

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"payment-gateway/internal/models"
)

// handleListAssets — GET /api/mobile/assets
func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := mobileDB(s.db).ListAssets(r.Context(), true)
	if err != nil {
		slog.Warn("mobile_assets_fallback", "err", err)
		assets = s.fallbackMobileAssets()
	}

	// Enrich with live price if PriceWorker available
	pw := s.PriceCache()
	type assetWithRate struct {
		Symbol   string  `json:"symbol"`
		Name     string  `json:"name"`
		Network  string  `json:"network"`
		Decimals int     `json:"decimals"`
		MinBRL   float64 `json:"min_amount_brl"`
		MaxBRL   float64 `json:"max_amount_brl"`
		FeeBPS   int     `json:"fee_bps"`
		PriceBRL float64 `json:"price_brl,omitempty"`
	}
	out := make([]assetWithRate, 0)
	for _, a := range assets {
		row := assetWithRate{
			Symbol:   a.Symbol,
			Name:     a.Name,
			Network:  a.Network,
			Decimals: a.Decimals,
			MinBRL:   a.MinAmount,
			MaxBRL:   a.MaxAmount,
			FeeBPS:   a.FeeBPS,
		}
		if pw != nil {
			row.PriceBRL = assetPriceInBRL(pw, a.Symbol)
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out, "count": len(out)})
}

func (s *Server) fallbackMobileAssets() []models.Asset {
	now := time.Now()
	usdtContract := ""
	if s != nil && s.cfg != nil {
		usdtContract = strings.TrimSpace(s.cfg.BscUsdtContract)
	}
	return []models.Asset{
		{Symbol: "USDT", Name: "Tether USD", Network: "BSC", ContractAddress: stringPtrOrNil(usdtContract), Decimals: 18, MinAmount: 10, MaxAmount: 50000, DailyLimit: 50000, MonthlyLimit: 500000, FeeBPS: 60, Active: true, CreatedAt: now},
		{Symbol: "USDC", Name: "USD Coin", Network: "BSC", ContractAddress: stringPtrOrNil(bscUSDCContractMobile), Decimals: 18, MinAmount: 10, MaxAmount: 50000, DailyLimit: 50000, MonthlyLimit: 500000, FeeBPS: 60, Active: true, CreatedAt: now},
		{Symbol: "BNB", Name: "BNB", Network: "BSC", Decimals: 18, MinAmount: 10, MaxAmount: 10000, DailyLimit: 10000, MonthlyLimit: 100000, FeeBPS: 60, Active: true, CreatedAt: now},
	}
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// handleGetAsset — GET /api/mobile/assets/{symbol}
func (s *Server) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))
	asset, err := mobileDB(s.db).GetAsset(r.Context(), symbol)
	if err != nil {
		slog.Error("erro interno", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "erro interno"})
		return
	}
	if asset == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "ativo não encontrado"})
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

// handleGetAssetRate — GET /api/mobile/assets/{symbol}/rate
// Returns live BRL/USD price for the requested asset.
func (s *Server) handleGetAssetRate(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToUpper(r.PathValue("symbol"))

	asset, err := mobileDB(s.db).GetAsset(r.Context(), symbol)
	if err != nil {
		slog.Error("erro interno", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "erro interno"})
		return
	}
	if asset == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "ativo não encontrado"})
		return
	}

	pw := s.PriceCache()
	var priceBRL, priceUSD float64
	if pw != nil {
		priceBRL = assetPriceInBRL(pw, symbol)
		priceUSD = assetPriceInUSD(pw, symbol)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"symbol":     symbol,
		"price_brl":  priceBRL,
		"price_usd":  priceUSD,
		"fee_bps":    asset.FeeBPS,
		"min_amount": asset.MinAmount,
		"max_amount": asset.MaxAmount,
		"updated_at": time.Now().Unix(),
	})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// assetPriceInBRL returns the BRL price for a given asset symbol using the
// PriceWorker's cached prices.
func assetPriceInBRL(pw interface{ GetPrice(string) float64 }, symbol string) float64 {
	switch strings.ToUpper(symbol) {
	case "USDT", "USDC", "BUSD":
		return pw.GetPrice("BRL") // USDT≈1 USD
	case "BTCB", "BTC":
		btcUSD := pw.GetPrice("BTCUSDT_SOURCE")
		usdtBRL := pw.GetPrice("BRL")
		if btcUSD > 0 && usdtBRL > 0 {
			return btcUSD * usdtBRL
		}
	case "ETH":
		// ETH not directly tracked yet — return 0 until PriceWorker is extended
		return 0
	case "EURC":
		usdtEUR := pw.GetPrice("USDTEUR")
		usdtBRL := pw.GetPrice("BRL")
		if usdtEUR > 0 && usdtBRL > 0 {
			return (1 / usdtEUR) * usdtBRL
		}
	}
	return 0
}

func assetPriceInUSD(pw interface{ GetPrice(string) float64 }, symbol string) float64 {
	switch strings.ToUpper(symbol) {
	case "USDT", "USDC", "BUSD":
		return 1
	case "BTCB", "BTC":
		return pw.GetPrice("BTCUSDT_SOURCE")
	case "EURC":
		if usdtEUR := pw.GetPrice("USDTEUR"); usdtEUR > 0 {
			return 1 / usdtEUR
		}
	}
	return 0
}
