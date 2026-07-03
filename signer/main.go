package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type TransferRequest struct {
	To              string `json:"to"`
	Amount          string `json:"amount"`
	TokenContract   string `json:"tokenContract"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type TransferResponse struct {
	TxHash string `json:"txHash"`
	From   string `json:"from"`
}

func main() {
	slog.Info("Inicializando bsc-signer isolado em Go...")
	cfg := LoadSignerConfig()

	if cfg.HMACSecret == "" || cfg.EVMPrivateKey == "" {
		slog.Error("FALHA CRÍTICA: HMAC_SECRET e EVM_PRIVATE_KEY são obrigatórios.")
		return
	}

	// Define a rota de transferência HD/EVM
	http.HandleFunc("/hd/transfer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		// 1. Captura os cabeçalhos de segurança militar
		ts := r.Header.Get("x-ts")
		nonce := r.Header.Get("x-nonce")
		hmacHeader := r.Header.Get("x-signer-hmac")

		// 2. Lê o corpo bruto da requisição
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Erro ao ler body", http.StatusBadRequest)
			return
		}

		// 3. Executa a validação criptográfica do HMAC
		if err := ValidateHMAC(cfg.HMACSecret, cfg.HMACMaxSkewSec, ts, nonce, hmacHeader, bodyBytes); err != nil {
			slog.Warn("Tentativa de acesso não autorizada bloqueada pelo HMAC", "motivo", err.Error())
			http.Error(w, "Não Autorizado: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// 4. Parseia o payload da transferência
		var req TransferRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			http.Error(w, "JSON Inválido", http.StatusBadRequest)
			return
		}

		slog.Info("Requisição HMAC validada com sucesso. Processando envio on-chain...", 
			"to", req.To, 
			"amount", req.Amount,
		)

		// TODO: Na Parte de Criptografia Avançada vamos plugar o driver 'go-ethereum' nativo
		// para assinar com a EVM_PRIVATE_KEY e transmitir via RPC_URL.
		// Por enquanto, geramos a resposta de stub idêntica à de produção:
		simulatedHash := "0x9f8e7d6c5b4a3f2e1d0c9b8a7f6e5d4c3b2a1f0e9d8c7b6a5f4e3d2c1b0a9f8e"

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransferResponse{
			TxHash: simulatedHash,
			From:   "0x00000000000000000000000000000000000Signer",
		})
	})

	serverAddr := ":" + cfg.Port
	slog.Info("Serviço bsc-signer rodando com segurança criptográfica", "porta", cfg.Port)
	
	server := &http.Server{
		Addr:         serverAddr,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	
	if err := server.ListenAndServe(); err != nil {
		slog.Error("Erro ao rodar servidor signer", "error", err)
	}
}