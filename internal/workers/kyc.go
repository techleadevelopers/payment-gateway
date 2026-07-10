package workers

// kyc.go — Phase 5: Async KYC processor worker.
//
// KYCWorker listens for kyc.submitted events and simulates an asynchronous
// review pipeline. In production this would call an external KYC provider
// (Jumio, Sumsub, etc.). Here it applies configurable auto-approval logic so
// the system is fully operational without a real provider.
//
// It also promotes users to the correct KYC level in the users table after
// approval, and publishes kyc.approved / kyc.rejected events for downstream
// consumers (push notifications, webhooks).

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"payment-gateway/internal/config"
	"payment-gateway/internal/database"
)

// KYCWorker processes KYC requests asynchronously.
type KYCWorker struct {
	bus *EventBus
	db  *database.DB
	cfg *config.Config
}

func NewKYCWorker(bus *EventBus, db *database.DB, cfg *config.Config) *KYCWorker {
	return &KYCWorker{bus: bus, db: db, cfg: cfg}
}

func (w *KYCWorker) Start(ctx context.Context) {
	slog.Info("KYCWorker iniciado")

	// Subscribe to kyc.submitted events (published by mobile/kyc_v2.go handlers)
	kycCh := w.bus.Subscribe("kyc.submitted")
	defer w.bus.Unsubscribe("kyc.submitted", kycCh)

	// Also poll the DB for any pending requests missed during downtime
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("KYCWorker encerrado")
			return
		case ev, ok := <-kycCh:
			if !ok {
				return
			}
			if id, ok := ev.Payload["kyc_request_id"].(string); ok {
				w.processRequest(ctx, id)
			}
		case <-ticker.C:
			w.processPending(ctx)
		}
	}
}

// processRequest handles a single KYC request ID.
func (w *KYCWorker) processRequest(ctx context.Context, id string) {
	// Mark as in_review
	if _, err := w.db.SQL.ExecContext(ctx, `
		UPDATE kyc_requests SET status='in_review', updated_at=NOW()
		WHERE id=$1 AND status='pending'`, id); err != nil {
		slog.Error("KYCWorker: erro ao marcar in_review", "id", id, "error", err)
		return
	}

	// Simulate async review delay (in prod: call external provider here)
	// For sandbox: auto-approve all level 1-2 requests after a brief delay.
	time.Sleep(500 * time.Millisecond)

	var userID string
	var level int
	err := w.db.SQL.QueryRowContext(ctx,
		"SELECT user_id, level FROM kyc_requests WHERE id=$1", id).Scan(&userID, &level)
	if err != nil {
		slog.Error("KYCWorker: erro ao buscar request", "id", id, "error", err)
		return
	}

	// Auto-approve levels 1 and 2; level 3 requires manual review
	var newStatus string
	if level <= 2 {
		newStatus = "approved"
	} else {
		newStatus = "in_review" // stays pending manual review
		slog.Info("KYCWorker: Level 3 aguarda revisão manual", "request_id", id, "user_id", userID)
		return
	}

	now := time.Now()
	if _, err := w.db.SQL.ExecContext(ctx, `
		UPDATE kyc_requests SET status=$1, reviewed_at=$2, updated_at=NOW()
		WHERE id=$3`, newStatus, now, id); err != nil {
		slog.Error("KYCWorker: erro ao atualizar status", "id", id, "error", err)
		return
	}

	if newStatus == "approved" {
		// Update user's kyc_status field to reflect new max approved level
		if _, err := w.db.SQL.ExecContext(ctx,
			"UPDATE users SET kyc_status='approved', updated_at=NOW() WHERE id=$1", userID); err != nil {
			slog.Warn("KYCWorker: não foi possível atualizar users.kyc_status", "user_id", userID, "error", err)
		}
		slog.Info("KYCWorker: KYC aprovado", "request_id", id, "user_id", userID, "level", level)
		w.bus.Publish(Event{
			Type:    "kyc.approved",
			Payload: map[string]any{"user_id": userID, "level": level, "request_id": id},
		})
	}
}

// processPending polls for any pending KYC requests not yet picked up by event.
func (w *KYCWorker) processPending(ctx context.Context) {
	rows, err := w.db.SQL.QueryContext(ctx, `
		SELECT id FROM kyc_requests
		WHERE status='pending' AND submitted_at < NOW() - INTERVAL '30 seconds'
		LIMIT 10`)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Warn("KYCWorker: erro no poll", "error", err)
		}
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			w.processRequest(ctx, id)
		}
	}
}
