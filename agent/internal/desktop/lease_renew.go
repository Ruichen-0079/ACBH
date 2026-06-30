package desktop

import (
	"context"
	"time"

	"github.com/Ruichen-0079/ACBH/agent/internal/coordinator"
)

const defaultLeaseRenewInterval = 15 * time.Second

// StartLeaseRenewLoop periodically calls EnsureActiveLease while ctx remains active.
// generation is updated in place when renewal succeeds. The returned stop function
// cancels the loop and waits for the goroutine to exit.
func StartLeaseRenewLoop(ctx context.Context, client *coordinator.Client, auth coordinator.ArtifactAuth, generation *int, gate *CoordinatorFeatureGate, interval time.Duration) func() {
	if interval <= 0 {
		interval = defaultLeaseRenewInterval
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				ensured, err := ensureActiveLeaseIfSupported(loopCtx, client, auth, generation, gate)
				if err != nil {
					continue
				}
				if ensured.Lease.LeaseValid && generation != nil {
					*generation = ensured.Lease.Generation
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}