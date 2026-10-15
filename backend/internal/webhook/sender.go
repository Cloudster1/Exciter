package webhook

import (
	"context"
	"log/slog"
	"time"
)

// Sender posts due deliveries until its context ends, and prunes the log.
// Any number may run: each delivery is leased before it is sent.
type Sender struct {
	svc      *Service
	log      *slog.Logger
	interval time.Duration
	pruned   time.Time
}

func NewSender(svc *Service, log *slog.Logger) *Sender {
	return &Sender{svc: svc, log: log, interval: SendInterval}
}

// Run sends until ctx ends.
func (s *Sender) Run(ctx context.Context) {
	s.log.Info("webhook sender started", "interval", s.interval)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		if _, err := s.svc.SendDue(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("due webhook deliveries could not be claimed", "error", err)
		}
		if time.Since(s.pruned) > pruneEvery {
			s.pruned = time.Now()
			if _, err := s.svc.Prune(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("old webhook deliveries could not be pruned", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			s.log.Info("webhook sender stopped")
			return
		case <-ticker.C:
		}
	}
}
