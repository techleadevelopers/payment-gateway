package workers

import (
	"context"
	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
)

type WorkerManager struct {
	Bus           *EventBus
	PriceWorker   *PriceWorker
	PayoutWorker  *PayoutWorker
	BuySendWorker *BuySendWorker
	OnchainWorker *OnchainWorker
	SweepWorker   *SweepWorker
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
		OnchainWorker: NewOnchainWorker(bus, db, cfg),
		SweepWorker:   NewSweepWorker(bus, db, cfg),
		db:            db,
		cfg:           cfg,
	}
}

// StartAll liga todas as chaves do motor ao mesmo tempo. Cada um cuidando da sua própria thread leve
func (wm *WorkerManager) StartAll(ctx context.Context) {
	go wm.PriceWorker.Start(ctx)
	go wm.PayoutWorker.Start(ctx)
	go wm.BuySendWorker.Start(ctx)
	go wm.OnchainWorker.Start(ctx)
	go wm.SweepWorker.Start(ctx)
}
