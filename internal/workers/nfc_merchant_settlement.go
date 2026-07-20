package workers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"payment-gateway/internal/certutil"
	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
	"payment-gateway/internal/httpclient"
	"payment-gateway/internal/resilience"
)

const (
	nfcSettlementPollSec = 5
	nfcSettlementMaxRuns = 4
)

type NFCMerchantSettlementWorker struct {
	bus    *EventBus
	db     *database.DB
	cfg    *config.Config
	client *http.Client
	dlq    *DeadLetterQueue
	sem    chan struct{}
}

func NewNFCMerchantSettlementWorker(bus *EventBus, db *database.DB, cfg *config.Config) *NFCMerchantSettlementWorker {
	return &NFCMerchantSettlementWorker{
		bus:    bus,
		db:     db,
		cfg:    cfg,
		client: nfcSettlementHTTPClient(cfg),
		dlq:    NewPersistentDLQ(db, 1000),
		sem:    make(chan struct{}, 4),
	}
}

func (w *NFCMerchantSettlementWorker) Start(ctx context.Context) {
	captureChan := w.bus.Subscribe("nfc.capture.completed")
	ticker := time.NewTicker(nfcSettlementPollSec * time.Second)
	defer ticker.Stop()
	w.dlq.StartPeriodicLog(ctx, 5*time.Minute)
	slog.Info("NFCMerchantSettlementWorker iniciado", "mode", w.mode())

	for {
		select {
		case <-ctx.Done():
			slog.Info("NFCMerchantSettlementWorker: encerrando")
			return
		case ev, ok := <-captureChan:
			if !ok {
				return
			}
			w.handleCaptureEvent(ctx, ev)
		case <-ticker.C:
			if w.automatic() {
				w.sweepDue(ctx)
			}
		}
	}
}

func (w *NFCMerchantSettlementWorker) handleCaptureEvent(ctx context.Context, ev Event) {
	settlementID, _ := ev.Payload["settlement_id"].(string)
	if settlementID == "" {
		return
	}
	if !w.automatic() {
		w.bus.Publish(Event{
			Type:    "nfc.settlement.manual_required",
			OrderID: ev.OrderID,
			Payload: map[string]any{
				"authorization_id": ev.OrderID,
				"settlement_id":    settlementID,
				"mode":             "manual",
			},
		})
		slog.Info("NFC settlement aguardando payout manual", "authorization_id", ev.OrderID, "settlement_id", settlementID)
		return
	}
	w.dispatch(ctx, settlementID)
}

func (w *NFCMerchantSettlementWorker) sweepDue(ctx context.Context) {
	settlements, err := w.db.GetDueMerchantSettlements(ctx, 50)
	if err != nil {
		slog.Error("NFC settlement: erro ao listar pendentes", "err", err)
		return
	}
	for _, settlement := range settlements {
		w.dispatch(ctx, settlement.ID)
	}
}

func (w *NFCMerchantSettlementWorker) dispatch(ctx context.Context, settlementID string) {
	select {
	case w.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	go func() {
		defer func() {
			<-w.sem
			if r := recover(); r != nil {
				slog.Error("NFC settlement: panic", "recover", r, "settlement_id", settlementID)
			}
		}()
		w.settleOne(ctx, settlementID)
	}()
}

func (w *NFCMerchantSettlementWorker) settleOne(ctx context.Context, settlementID string) {
	start := time.Now()
	settlement, claimed, err := w.db.ClaimMerchantSettlement(ctx, settlementID)
	if err != nil {
		slog.Error("NFC settlement: claim falhou", "settlement_id", settlementID, "err", err)
		w.dlq.Push(Event{Type: "nfc.settlement.failed", OrderID: settlementID}, 1, err.Error())
		return
	}
	if !claimed || settlement == nil {
		return
	}
	if settlement.RetryCount > nfcSettlementMaxRuns {
		_ = w.db.MarkMerchantSettlementFailed(ctx, settlement.ID, "max settlement attempts exceeded", true)
		return
	}
	if strings.TrimSpace(settlement.TargetPixKey) == "" {
		_ = w.db.MarkMerchantSettlementFailed(ctx, settlement.ID, "merchant settlement Pix key missing", true)
		w.bus.Publish(Event{Type: "nfc.settlement.failed", OrderID: settlement.AuthorizationID, Payload: map[string]any{
			"settlement_id": settlement.ID,
			"permanent":     true,
			"error":         "merchant settlement Pix key missing",
		}})
		return
	}

	var providerRef, providerStatus string
	retryCfg := resilience.RetryConfig{
		MaxAttempts: 1,
		BaseDelay:   0,
		MaxDelay:    0,
		Multiplier:  1,
		Jitter:      false,
	}
	err = resilience.DoWithContext(ctx, retryCfg, "nfc.settlement."+settlement.ID, isRetryableNFCSettlementError, func(ctx context.Context) error {
		ref, status, callErr := w.callEfiPixSend(ctx, settlement)
		if callErr != nil {
			return callErr
		}
		providerRef = ref
		providerStatus = status
		return nil
	})
	if err != nil {
		permanent := isPermanentNFCSettlementError(err)
		_ = w.db.MarkMerchantSettlementFailed(ctx, settlement.ID, err.Error(), permanent)
		w.bus.Publish(Event{Type: "nfc.settlement.failed", OrderID: settlement.AuthorizationID, Payload: map[string]any{
			"settlement_id": settlement.ID,
			"permanent":     permanent,
			"error":         err.Error(),
			"attempts":      settlement.RetryCount,
		}})
		if !permanent {
			w.dlq.Push(Event{Type: "nfc.settlement.failed", OrderID: settlement.AuthorizationID}, settlement.RetryCount, err.Error())
		}
		return
	}

	if err := w.db.MarkMerchantSettlementConfirmed(ctx, settlement.ID, providerRef, providerStatus, providerRef); err != nil {
		slog.Error("NFC settlement: CRITICAL payout confirmado mas persistencia falhou", "settlement_id", settlement.ID, "provider_reference", providerRef, "err", err)
		return
	}
	w.bus.Publish(Event{Type: "nfc.settlement.confirmed", OrderID: settlement.AuthorizationID, Payload: map[string]any{
		"settlement_id":      settlement.ID,
		"provider":           settlement.Provider,
		"provider_reference": providerRef,
		"provider_status":    providerStatus,
		"amount_brl_minor":   settlement.AmountBRLMinor,
	}})
	slog.Info("NFC settlement confirmado", "settlement_id", settlement.ID, "authorization_id", settlement.AuthorizationID, "duration_ms", time.Since(start).Milliseconds())
}

func (w *NFCMerchantSettlementWorker) callEfiPixSend(ctx context.Context, settlement *database.MerchantSettlement) (string, string, error) {
	if w.cfg.EfiClientID == "" || w.cfg.EfiClientSecret == "" || w.cfg.EfiPixKey == "" {
		return "", "", fmt.Errorf("Efí Pix Send nao configurado")
	}
	token, err := w.getEfiToken(ctx)
	if err != nil {
		return "", "", fmt.Errorf("efi auth: %w", err)
	}
	payload := map[string]any{
		"valor": fmt.Sprintf("%.2f", float64(settlement.AmountBRLMinor)/100),
		"pagador": map[string]any{
			"chave":       w.cfg.EfiPixKey,
			"infoPagador": fmt.Sprintf("ChainFX Tap %s", settlement.AuthorizationID),
		},
		"favorecido": map[string]any{
			"chave": settlement.TargetPixKey,
		},
	}
	if doc := onlyDigitsWorker(settlement.TargetDocument); doc != "" {
		payload["favorecido"].(map[string]any)["cpf"] = doc
	}
	body, _ := json.Marshal(payload)
	pathID := settlement.IdempotencyKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		strings.TrimRight(w.cfg.EfiApiBaseURL, "/")+"/v3/gn/pix/"+pathID, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := w.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("efi pix send request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("efi pix send status %d: %s", resp.StatusCode, string(respBody))
	}
	var result struct {
		IDEnvio    string `json:"idEnvio"`
		E2EID      string `json:"e2eId"`
		EndToEndID string `json:"endToEndId"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("efi pix send response parse: %w", err)
	}
	ref := firstNonEmpty(result.E2EID, result.EndToEndID, result.IDEnvio)
	if ref == "" {
		return "", "", fmt.Errorf("efi pix send: provider reference vazio")
	}
	return ref, firstNonEmpty(result.Status, "SUBMITTED"), nil
}

func (w *NFCMerchantSettlementWorker) getEfiToken(ctx context.Context) (string, error) {
	raw := []byte(`{"grant_type":"client_credentials"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(w.cfg.EfiApiBaseURL, "/")+"/oauth/token", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(w.cfg.EfiClientID, w.cfg.EfiClientSecret)
	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("efi token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("efi token status %d: %s", resp.StatusCode, string(body))
	}
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("efi: access_token vazio")
	}
	return result.AccessToken, nil
}

func (w *NFCMerchantSettlementWorker) automatic() bool {
	mode := w.mode()
	return mode == "efi" || mode == "automatic" || mode == "auto"
}

func (w *NFCMerchantSettlementWorker) mode() string {
	if w == nil || w.cfg == nil {
		return "manual"
	}
	return strings.ToLower(strings.TrimSpace(w.cfg.NFCSettlementMode))
}

func nfcSettlementHTTPClient(cfg *config.Config) *http.Client {
	if cfg == nil {
		return httpclient.Default()
	}
	cert, err := certutil.LoadCertificate(cfg.EfiCertificatePath, cfg.EfiCertificateKey, cfg.EfiCertificateP12, cfg.EfiCertificatePass)
	if err != nil {
		return httpclient.Default()
	}
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}},
	}
}

func isRetryableNFCSettlementError(err error) bool {
	if err == nil {
		return false
	}
	return !isPermanentNFCSettlementError(err)
}

func isPermanentNFCSettlementError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{"chave_invalida", "cpf_invalido", "conta_bloqueada", "kyc", "nao configurado", "pix key missing", "status 400", "status 401", "status 403", "status 404", "status 422"} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func onlyDigitsWorker(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
