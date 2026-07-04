package workers

import (
	"context"
	"log/slog"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
)

type OnchainWorker struct {
	bus *EventBus
	db  *database.DB
	cfg *config.Config
}

func NewOnchainWorker(bus *EventBus, db *database.DB, cfg *config.Config) *OnchainWorker {
	return &OnchainWorker{bus: bus, db: db, cfg: cfg}
}

func (ow *OnchainWorker) Start(ctx context.Context) {
	slog.Info("OnchainWorker BSC inicializado em background.")
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Desligando OnchainWorker BSC")
			return
		case <-ticker.C:
			ow.pollBscEvents(ctx)
		}
	}
}

func (ow *OnchainWorker) pollBscEvents(ctx context.Context) {
	if ow.cfg.BscUsdtContract == "" || ow.cfg.BscRpcUrls == "" {
		slog.Warn("BSC_USDT_CONTRACT ou BSC_RPC_URLS ausentes; pulando listener on-chain BSC.")
		return
	}
	slog.Debug("Listener on-chain BSC aguardando implementacao de logs BEP20", "contract", ow.cfg.BscUsdtContract)
}
