package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleQuote(w http.ResponseWriter, r *http.Request) {
	markLegacyRoute(w, r, "/quote")
	var amountBRL, amountUSD, amountFiat float64
	mode := "buy"
	asset := "USDT"
	fiatCurrency := "BRL"
	paymentMethod := "pix"
	if r.Method == http.MethodGet {
		amountBRL, _ = strconv.ParseFloat(r.URL.Query().Get("amountBRL"), 64)
		amountUSD, _ = strconv.ParseFloat(r.URL.Query().Get("amountUSD"), 64)
		amountFiat, _ = strconv.ParseFloat(r.URL.Query().Get("amountFiat"), 64)
		mode = defaultString(r.URL.Query().Get("mode"), mode)
		asset = defaultString(r.URL.Query().Get("asset"), asset)
		fiatCurrency = defaultString(r.URL.Query().Get("fiatCurrency"), fiatCurrency)
		paymentMethod = defaultString(r.URL.Query().Get("paymentMethod"), paymentMethod)
	} else {
		var req struct {
			AmountBRL     float64 `json:"amountBRL"`
			AmountUSD     float64 `json:"amountUSD"`
			AmountFiat    float64 `json:"amountFiat"`
			FiatCurrency  string  `json:"fiatCurrency"`
			PaymentMethod string  `json:"paymentMethod"`
			Mode          string  `json:"mode"`
			Asset         string  `json:"asset"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "JSON invalido"})
			return
		}
		amountBRL = req.AmountBRL
		amountUSD = req.AmountUSD
		amountFiat = req.AmountFiat
		mode = defaultString(req.Mode, mode)
		asset = defaultString(req.Asset, asset)
		fiatCurrency = defaultString(req.FiatCurrency, fiatCurrency)
		paymentMethod = defaultString(req.PaymentMethod, paymentMethod)
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if mode != "buy" && mode != "sell" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "modo invalido"})
		return
	}
	if asset != "USDT" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "asset nao suportado nesta fase"})
		return
	}
	fiatCurrency, paymentMethod, amountFiat = normalizePaymentRail(fiatCurrency, paymentMethod, amountFiat, amountBRL, amountUSD)
	if mode == "sell" {
		fiatCurrency, paymentMethod = "BRL", "pix"
	}
	if fiatCurrency == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "rail de pagamento nao suportado"})
		return
	}
	if amountFiat <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "amountFiat deve ser maior que zero"})
		return
	}
	if mode != "sell" && fiatCurrency == "BRL" && (amountFiat < s.buyMinBRL() || amountFiat > s.cfg.OrderMaxBrl) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("valor fora dos limites (%.2f - %.2f BRL)", s.buyMinBRL(), s.cfg.OrderMaxBrl)})
		return
	}
	marketRate := s.workers.PriceWorker.GetPrice(fiatCurrency)
	if marketRate <= 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "cotacao ainda nao carregada"})
		return
	}
	if mode == "sell" {
		amountUSDT := amountFiat
		rate, payoutBRL, spreadBRL := s.sellQuote(amountUSDT, marketRate)
		if payoutBRL < s.cfg.OrderMinBrl || payoutBRL > s.cfg.OrderMaxBrl {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("payout fora dos limites (%.2f - %.2f BRL)", s.cfg.OrderMinBrl, s.cfg.OrderMaxBrl)})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"mode":              mode,
			"asset":             asset,
			"amountFiat":        payoutBRL,
			"subtotalFiat":      payoutBRL,
			"fiatCurrency":      "BRL",
			"paymentMethod":     paymentMethod,
			"feeFiat":           spreadBRL,
			"spreadFiat":        spreadBRL,
			"totalFiat":         payoutBRL,
			"payoutFiat":        payoutBRL,
			"sellPolicy":        s.sellPolicy(marketRate, rate),
			"rate":              rate,
			"marketRate":        roundRate(marketRate),
			"cryptoAmount":      amountUSDT,
			"rateLockExpiresAt": time.Now().Add(time.Duration(s.cfg.RateLockSec) * time.Second),
		})
		return
	}
	rate := s.buyRate(marketRate)
	fee := s.transactionFee(amountFiat, fiatCurrency, rate)
	payout := amountFiat
	if payout <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "valor insuficiente apos taxa"})
		return
	}
	totalFiat := amountFiat + fee
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":              mode,
		"asset":             asset,
		"amountFiat":        totalFiat,
		"subtotalFiat":      amountFiat,
		"fiatCurrency":      fiatCurrency,
		"paymentMethod":     paymentMethod,
		"feeFiat":           fee,
		"totalFiat":         totalFiat,
		"payoutFiat":        payout,
		"feePolicy":         s.feePolicy(fiatCurrency, rate),
		"feeBreakdown":      s.buyFeeBreakdown(amountFiat),
		"rate":              rate,
		"marketRate":        roundRate(marketRate),
		"cryptoAmount":      payout / rate,
		"rateLockExpiresAt": time.Now().Add(time.Duration(s.cfg.RateLockSec) * time.Second),
	})
}
