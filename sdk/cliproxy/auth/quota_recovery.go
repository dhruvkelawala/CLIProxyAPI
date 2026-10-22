package auth

import (
	"context"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

const quotaRecoveryInterval = time.Minute

func quotaRecoveryCandidate(auth *Auth, now time.Time) bool {
	return auth != nil && !auth.Disabled && auth.Status != StatusDisabled &&
		!hasUnauthorizedAuthFailure(auth) && auth.Quota.Exceeded &&
		auth.Quota.NextRecoverAt.After(now) &&
		(auth.LastError == nil || auth.LastError.HTTPStatus == http.StatusTooManyRequests)
}

func (m *Manager) runQuotaRecovery(ctx context.Context) {
	ticker := time.NewTicker(quotaRecoveryInterval)
	defer ticker.Stop()
	slots := make(chan struct{}, 4)
	for {
		m.checkQuotaRecovery(ctx, slots)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) checkQuotaRecovery(ctx context.Context, slots chan struct{}) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, auth := range m.auths {
		if ctx.Err() != nil {
			return
		}
		if !quotaRecoveryCandidate(auth, time.Now()) {
			continue
		}
		exec, _ := m.executorLocked(executorKeyFromAuth(auth))
		checker, ok := exec.(QuotaRecoveryChecker)
		if !ok {
			continue
		}
		if _, busy := m.quotaRecoveryJobs.LoadOrStore(auth.ID, struct{}{}); busy {
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			m.quotaRecoveryJobs.Delete(auth.ID)
			continue
		}
		snapshot := auth.Clone()
		go func() {
			defer func() {
				<-slots
				m.quotaRecoveryJobs.Delete(snapshot.ID)
			}()
			recovered, errCheck := checker.QuotaRecovered(ctx, snapshot)
			if errCheck != nil {
				log.WithError(errCheck).WithField("auth_id", snapshot.ID).Debug("upstream quota recovery check failed")
				return
			}
			if !recovered || ctx.Err() != nil {
				return
			}
			updated, _, errReset := m.resetQuota(ctx, snapshot.ID, snapshot)
			if errReset != nil {
				log.WithError(errReset).WithField("auth_id", snapshot.ID).Warn("failed to persist recovered quota")
			} else if updated != nil {
				log.WithField("auth_id", snapshot.ID).Info("restored routing after upstream quota recovery")
			}
		}()
	}
}
