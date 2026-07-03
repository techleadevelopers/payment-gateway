package workers

import (
	"context"
	"encoding/json"
	"log/slog" // Logger estruturado nativo do Go (Substitui o Pino)
	"net/http"
	"sync"
	"time"
)

type PriceWorker struct {
	bus       *EventBus
	client    *http.Client
	mu        sync.RWMutex
	lastPrice float64
}

type CoinGeckoResponse struct {
	Tether struct {
		Brl float64 `json:"brl"`
	} `json:"tether"`
}

func NewPriceWorker(bus *EventBus) *PriceWorker {
	return &PriceWorker{
		bus: bus,
		client: &http.Client{
			Timeout: 5 * time.Second, // Hardening: Evita conexões presas consumindo RAM
		},
	}
}

// GetCurrentPrice permite que a API leia o cache em memória instantaneamente (Latência zero)
func (pw *PriceWorker) GetCurrentPrice() float64 {
	pw.mu.RLock()
	defer pw.mu.RUnlock()
	return pw.lastPrice
}

// Start inicia o loop em background usando contextos para desligamento limpo (graceful shutdown)
func (pw *PriceWorker) Start(ctx context.Context) {
	slog.Info("PriceWorker inicializado com sucesso.")
	
	// Executa a primeira carga imediatamente no boot
	pw.fetchPrice()

	// Ticker de 60 segundos (idêntico ao TTL do seu Node)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Desligando PriceWorker de forma segura...")
			return
		case <-ticker.C:
			pw.fetchPrice()
		}
	}
}

func (pw *PriceWorker) fetchPrice() {
	start := time.Now()
	
	req, err := http.NewRequestWithContext(context.Background(), "GET", "https://api.coingecko.com/api/v3/simple/price?ids=tether&vs_currencies=brl", nil)
	if err != nil {
		slog.Error("Erro ao criar requisição de preço", "error", err)
		return
	}

	resp, err := pw.client.Do(req)
	if err != nil {
		slog.Error("Erro na requisição ao CoinGecko", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("CoinGecko retornou status inválido", "status", resp.StatusCode)
		return
	}

	var data CoinGeckoResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Error("Erro ao parsear JSON do preço", "error", err)
		return
	}

	pw.mu.Lock()
	pw.lastPrice = data.Tether.Brl
	pw.mu.Unlock()

	// Métrica de latência básica integrada nos logs estruturados
	slog.Info("Preço USDT atualizado com sucesso", 
		"price", data.Tether.Brl, 
		"duration_ms", time.Since(start).Milliseconds(),
	)

	// Publica no barramento para quem quiser escutar
	pw.bus.Publish(Event{
		Type: "price.updated",
		Payload: map[string]interface{}{"price": data.Tether.Brl},
	})
}