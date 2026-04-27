package support

import (
	"context"
	"log"
	"time"
)

func RunRoutingWorker(ctx context.Context, logger *log.Logger, db Querier, interval time.Duration) {
	if !isSupportRoutingEnabled() {
		logger.Println("support routing worker disabled")
		return
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger.Printf("support routing worker started (interval=%s)", interval)

	for {
		select {
		case <-ctx.Done():
			logger.Println("support routing worker stopped")
			return
		case <-ticker.C:
			if err := db.SweepExpiredSupportOffers(ctx); err != nil {
				logger.Printf("support routing sweep failed: %v", err)
			}
		}
	}
}
