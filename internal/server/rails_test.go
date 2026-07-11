package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
	"payment-gateway/internal/workers"
)

func TestNormalizePaymentRailPixBRL(t *testing.T) {
	currency, method, amount := normalizePaymentRail("", "", 0, 150, 0)
	if currency != "BRL" || method != "pix" || amount != 150 {
		t.Fatalf("unexpected rail: %s %s %.2f", currency, method, amount)
	}
}

func TestNormalizePaymentRailStripeUSD(t *testing.T) {
	currency, method, amount := normalizePaymentRail("USD", "stripe", 0, 0, 25)
	if currency != "USD" || method != "stripe" || amount != 25 {
		t.Fatalf("unexpected rail: %s %s %.2f", currency, method, amount)
	}
}

func TestNormalizePaymentRailRejectsUnsupported(t *testing.T) {
	currency, method, amount := normalizePaymentRail("USD", "pix", 10, 0, 0)
	if currency != "" || method != "" || amount != 0 {
		t.Fatalf("expected unsupported rail to be rejected")
	}
}

func TestValidStripeSignature(t *testing.T) {
	secret := "whsec_test"
	body := []byte(`{"id":"evt_123"}`)
	ts := fmt.Sprintf("%d", time.Now().Unix())
	header := stripeHeader(secret, ts, body)

	if !validStripeSignature(secret, body, header, 5*time.Minute) {
		t.Fatal("expected valid stripe signature")
	}
	if validStripeSignature(secret, []byte(`{"id":"evt_tampered"}`), header, 5*time.Minute) {
		t.Fatal("expected tampered body to fail signature validation")
	}
}

func TestValidStripeSignatureRejectsExpiredTimestamp(t *testing.T) {
	secret := "whsec_test"
	body := []byte(`{"id":"evt_123"}`)
	ts := fmt.Sprintf("%d", time.Now().Add(-10*time.Minute).Unix())

	if validStripeSignature(secret, body, stripeHeader(secret, ts, body), 5*time.Minute) {
		t.Fatal("expected expired stripe signature to be rejected")
	}
}

func TestCustomerAccessTokenPrefersHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/buy/id?accessToken=query-token", nil)
	req.Header.Set("X-Customer-Access-Token", "header-token")

	if got := customerAccessToken(req); got != "header-token" {
		t.Fatalf("expected header token, got %q", got)
	}
}

func TestPixBuyWebhookRequiresProviderID(t *testing.T) {
	secret := "pix-secret"
	body := []byte(`{"buyId":"00000000-0000-4000-8000-000000000001","status":"concluido"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/pix/webhook/buy", strings.NewReader(string(body)))
	req.Header.Set("x-efi-signature", rawHMAC(secret, body))
	rec := httptest.NewRecorder()

	s := &Server{cfg: &config.Config{PixWebhookSecret: secret}, workers: &workers.WorkerManager{}}
	s.handlePixWebhookBuy(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing providerId to be rejected with 400, got %d", rec.Code)
	}
}

func TestEmailTestRequiresInternalHMAC(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/internal/email/test", strings.NewReader(`{"to":"ops@example.com"}`))
	rec := httptest.NewRecorder()

	s := &Server{cfg: &config.Config{SignerHmacSecret: "internal-secret"}}
	s.handleEmailTest(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected unsigned email test to be rejected, got %d", rec.Code)
	}
}

func TestMCPInitializeRouteUsesAPIKeyAuth(t *testing.T) {
	cfg := &config.Config{
		ChainFXLiveSecretKeys: "sk_live_test_mcp",
		ChainFXRequireAPIKey:  true,
	}
	wm := workers.NewWorkerManager(nil, cfg, nil)
	s := New(cfg, nil, wm, nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp/initialize", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer sk_live_test_mcp")
	rec := httptest.NewRecorder()

	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected MCP initialize route to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"chainfx-mcp"`) {
		t.Fatalf("expected MCP server info in response, got %s", rec.Body.String())
	}
}

func TestAgentDiscoveryAdvertisesSixPercentFee(t *testing.T) {
	s := &Server{cfg: &config.Config{TreasuryHot: "0x000000000000000000000000000000000000dEaD"}}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/ai-services.json", nil)
	rec := httptest.NewRecorder()

	s.handleAIServicesWellKnown(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected discovery to return 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"gatewayFeeBps":600`) {
		t.Fatalf("expected 600 bps ChainFX fee, got %s", body)
	}
	if !strings.Contains(body, "ChainFX 0.60") {
		t.Fatalf("expected 10 USDT fee example with ChainFX 0.60, got %s", body)
	}
}

func TestUSDTAmountToWeiUsesBSCUSDTDecimals(t *testing.T) {
	got := usdtAmountToWei(10)
	want := "10000000000000000000"
	if got.String() != want {
		t.Fatalf("expected %s wei, got %s", want, got.String())
	}
}

func TestAgentTradeQuoteCalculatesHighFeeForReceiveAmount(t *testing.T) {
	amounts, err := calculateAgentTradeAmounts(500, "receive", agentGatewayFeeBps)
	if err != nil {
		t.Fatal(err)
	}
	if amounts.ReceiveAmount != 500 {
		t.Fatalf("expected receive amount 500, got %.6f", amounts.ReceiveAmount)
	}
	if amounts.PayAmount != 531.914894 {
		t.Fatalf("expected pay amount 531.914894 with 6%% fee, got %.6f", amounts.PayAmount)
	}
	if amounts.ChainFXFeeAmount != 31.914894 {
		t.Fatalf("expected ChainFX fee 31.914894, got %.6f", amounts.ChainFXFeeAmount)
	}
}

func TestAgentDiscoveryAdvertisesLiquidityRail(t *testing.T) {
	s := &Server{cfg: &config.Config{TreasuryHot: "0x000000000000000000000000000000000000dEaD"}}
	req := httptest.NewRequest(http.MethodGet, "/.well-known/ai-services.json", nil)
	rec := httptest.NewRecorder()

	s.handleAIServicesWellKnown(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/agent/v1/trade/quote") {
		t.Fatalf("expected agent trade quote discovery, got %s", body)
	}
	if !strings.Contains(body, "/agent/v1/assets") || !strings.Contains(body, "enabled BSC stablecoin pairs") {
		t.Fatalf("expected supported liquidity rail, got %s", body)
	}
}

func TestAgentAssetsFallbackListsStablecoinsWithSixPercentFee(t *testing.T) {
	s := &Server{cfg: &config.Config{BscUsdtContract: "0x55d398326f99059fF775485246999027B3197955"}}
	assets := s.fallbackAgentTradeAssets()
	if len(assets) != 3 {
		t.Fatalf("expected 3 seeded stablecoins, got %d", len(assets))
	}
	seen := map[string]bool{}
	for _, asset := range assets {
		seen[asset.Symbol] = true
		if asset.FeeBps != agentGatewayFeeBps {
			t.Fatalf("expected %s fee %d, got %d", asset.Symbol, agentGatewayFeeBps, asset.FeeBps)
		}
		if asset.Symbol == "BUSD" && (asset.Enabled || asset.Status != "legacy") {
			t.Fatalf("expected BUSD to be legacy disabled, got enabled=%v status=%s", asset.Enabled, asset.Status)
		}
	}
	for _, symbol := range []string{"USDT", "USDC", "BUSD"} {
		if !seen[symbol] {
			t.Fatalf("expected %s in fallback assets", symbol)
		}
	}
}

func TestAgentCapabilitiesExposeMachineReadableLifecycle(t *testing.T) {
	s := &Server{cfg: &config.Config{BscUsdtContract: "0x55d398326f99059fF775485246999027B3197955"}}
	req := httptest.NewRequest(http.MethodGet, "/agent/v1/capabilities", nil)
	rec := httptest.NewRecorder()

	s.handleAgentCapabilities(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected capabilities 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, expected := range []string{
		"stablecoin_exchange",
		"api_access_purchase",
		"discover_capabilities",
		"create_trade_intent",
		"wallet_signature_headers",
		"/agent/v1/trade/quote",
		"/agent/v1/assets",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected capabilities to contain %q, got %s", expected, body)
		}
	}
}

func TestAgentCapabilitiesAdvertiseMarketplacePurchase(t *testing.T) {
	s := &Server{cfg: &config.Config{BscUsdtContract: "0x55d398326f99059fF775485246999027B3197955"}}
	req := httptest.NewRequest(http.MethodGet, "/agent/v1/capabilities", nil)
	rec := httptest.NewRecorder()

	s.handleAgentCapabilities(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected capabilities 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, expected := range []string{
		"marketplace_api_purchase",
		"discover_marketplace",
		"list_products",
		"create_purchase",
		"verify_receipt",
		"receive_access_grant",
		"wallet_signature_auth",
		"planned",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected capabilities to contain %q, got %s", expected, body)
		}
	}
}

func TestAgentCapabilitiesAdvertiseCapabilityExchange(t *testing.T) {
	s := &Server{cfg: &config.Config{BscUsdtContract: "0x55d398326f99059fF775485246999027B3197955"}}
	req := httptest.NewRequest(http.MethodGet, "/agent/v1/capabilities", nil)
	rec := httptest.NewRecorder()

	s.handleAgentCapabilities(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected capabilities 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, expected := range []string{
		"capability_exchange",
		"/marketplace/capabilities",
		"semantic_memory",
		"llm_chat",
		"document_ocr",
		"payments_fx",
		"capability_discovery",
		"agent_connect",
		"/agent/connect",
		"/agent/v1/capabilities/{capability}/execute",
		"capabilityRouter",
		"mock_dev",
		"mock_provider_execution",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected capabilities to contain %q, got %s", expected, body)
		}
	}
}

func TestAgentTradeQuoteResponseExplainsGrossPaymentFee(t *testing.T) {
	expires := time.Now().UTC().Add(time.Minute)
	resp := agentTradeQuoteResponse(&database.AgentTradeIntent{
		ID:                   "00000000-0000-4000-8000-000000000001",
		PayAsset:             "USDC",
		ReceiveAsset:         "USDT",
		PayAmount:            531.914894,
		ReceiveAmount:        500,
		ChainFXFeeAmount:     31.914894,
		FeeBps:               agentGatewayFeeBps,
		Network:              "BSC",
		PaymentAddress:       "0x000000000000000000000000000000000000dead",
		DestinationWallet:    "0x000000000000000000000000000000000000beef",
		PayTokenContract:     "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d",
		ReceiveTokenContract: "0x55d398326f99059ff775485246999027b3197955",
		Nonce:                "tr_test",
		RequestHash:          "hash",
		ExpiresAt:            expires,
	}, "https://example.com")
	if resp["feeCalculation"] != "deducted_from_gross_payment" {
		t.Fatalf("expected gross payment fee calculation, got %#v", resp["feeCalculation"])
	}
	if resp["overpaymentPolicy"] == "" {
		t.Fatal("expected overpayment policy in quote response")
	}
}

func rawHMAC(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func stripeHeader(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
