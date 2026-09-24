package execution

import (
	"context"
	"log/slog"
	"time"
)

type sessionPurger interface {
	PurgeExpiredSessions(ctx context.Context, limit int) (int64, error)
}

// Maintenance reclaims expired federated sessions on a fixed cadence. It is
// bounded per run so a large backlog cannot hold long transactions.
type Maintenance struct {
	store    sessionPurger
	logger   *slog.Logger
	interval time.Duration
}

func NewMaintenance(repository sessionPurger, logger *slog.Logger, interval time.Duration) *Maintenance {
	return &Maintenance{store: repository, logger: logger, interval: interval}
}

func (m *Maintenance) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		purged, err := m.store.PurgeExpiredSessions(runCtx, 1000)
		cancel()
		if err != nil {
			m.logger.Warn("session purge failed", "error", err)
			continue
		}
		if purged > 0 {
			m.logger.Info("expired sessions purged", "count", purged)
		}
	}
}
