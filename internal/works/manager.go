package workers

import (
	"context"
	"meu-gateway-go/internal/config"
	"meu-gateway-go/internal/database"
)

type WorkerManager struct {
	Bus           *EventBus
	PriceWorker   *PriceWorker
	PayoutWorker  *PayoutWorker
	BuySendWorker *BuySendWorker
	db            *database.DB
	cfg           *config.Config
}

func NewWorkerManager(db *database.DB, cfg *config.Config) *WorkerManager {
	bus := NewEventBus()
	
	return &WorkerManager{
		Bus:           bus,
		PriceWorker:   NewPriceWorker(bus),
		PayoutWorker:  NewPayoutWorker(bus, db, cfg),
		BuySendWorker: NewBuySendWorker(bus, db, cfg),
		db:            db,
		cfg:           cfg,
	}
}

// StartAll dispara todos os workers em background dentro de Goroutines isoladas e protegidas
func (wm *WorkerManager) StartAll(ctx context.Context) {
	// Cada comando 'go' inicia um loop concorrente infinito isolado em memória
	go wm.PriceWorker.Start(ctx)
	go wm.PayoutWorker.Start(ctx)
	go wm.BuySendWorker.Start(ctx)
}