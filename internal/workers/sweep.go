package workers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
)

type SweepWorker struct {
	bus    *EventBus
	db     *database.DB
	cfg    *config.Config
	client *http.Client
}

type SweepPayload struct {
	DerivationIndex int    `json:"derivationIndex"`
	To              string `json:"to"`
	Amount          string `json:"amount"`
	TokenContract   string `json:"tokenContract"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

func NewSweepWorker(bus *EventBus, db *database.DB, cfg *config.Config) *SweepWorker {
	return &SweepWorker{
		bus: bus,
		db:  db,
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (sw *SweepWorker) Start(ctx context.Context) {
	slog.Info("SweepWorker inicializado com segurança anti-replay.")

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Desligando SweepWorker...")
			return
		case <-ticker.C:
			sw.executeSweeps(ctx)
		}
	}
}

func (sw *SweepWorker) executeSweeps(ctx context.Context) {
	if sw.cfg.SignerUrl == "" || sw.cfg.TreasuryHot == "" {
		slog.Warn("SweepWorker suspenso: SIGNER_URL ou TREASURY_HOT ausentes.")
		return
	}

	// Exemplo de payload a ser assinado para varrer uma carteira filha índice #42
	payload := SweepPayload{
		DerivationIndex: 42,
		To:              sw.cfg.TreasuryHot,
		Amount:          "150.50", // 150.50 USDT
		TokenContract:   sw.cfg.TronUsdtContract,
		IdempotencyKey:  "sweep-uuid-da-transacao",
	}

	bodyBytes, _ := json.Marshal(payload)

	// --- CÁLCULO CRIPTOGRÁFICO DO HMAC ANTI-REPLAY ---
	ts := fmt.Sprintf("%d", time.Now().Unix())

	// Gera um Nonce aleatório seguro de 8 bytes (substitui o crypto.randomBytes do Node)
	nonceBytes := make([]byte, 8)
	_, _ = rand.Read(nonceBytes)
	nonce := hex.EncodeToString(nonceBytes)

	// Montagem do payload de assinatura: ts + "." + nonce + "." + rawBody
	signatureRaw := fmt.Sprintf("%s.%s.%s", ts, nonce, string(bodyBytes))

	mac := hmac.New(sha256.New, []byte(sw.cfg.SignerHmacSecret))
	mac.Write([]byte(signatureRaw))
	computedHmac := hex.EncodeToString(mac.Sum(nil))
	// -------------------------------------------------

	req, err := http.NewRequestWithContext(ctx, "POST", sw.cfg.SignerUrl+"/hd/transfer", bytes.NewBuffer(bodyBytes))
	if err != nil {
		slog.Error("Erro ao criar request de sweep", "error", err)
		return
	}

	// Injeta os headers de segurança militar exigidos pelo seu microsserviço Signer
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-ts", ts)
	req.Header.Set("x-nonce", nonce)
	req.Header.Set("x-signer-hmac", computedHmac)

	slog.Info("Disparando comando de Sweep seguro para o Signer", "index", payload.DerivationIndex)

	resp, err := sw.client.Do(req)
	if err != nil {
		slog.Error("Falha crítica na comunicação com o Signer HD", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		slog.Info("Varredura (Sweep) executada e assinada com sucesso na blockchain.")
	} else {
		slog.Error("O serviço Signer rejeitou a transação de Sweep", "status", resp.StatusCode)
	}
}
