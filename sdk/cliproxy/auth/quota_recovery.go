package auth

import (
	"context"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	quotaRecoveryInterval   = time.Minute
	quotaRecoveryMaxBackoff = 15 * time.Minute
)

type quotaRecoveryJob struct {
	running   bool
	delay     time.Duration
	nextCheck time.Time
}

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
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for id, job := range m.quotaRecoveryJobs {
		if !job.running && !job.nextCheck.After(now) && !quotaRecoveryCandidate(m.auths[id], now) {
			delete(m.quotaRecoveryJobs, id)
		}
	}
	for _, auth := range m.auths {
		if ctx.Err() != nil {
			return
		}
		if !quotaRecoveryCandidate(auth, now) {
			continue
		}
		exec, _ := m.executorLocked(executorKeyFromAuth(auth))
		checker, ok := exec.(QuotaRecoveryChecker)
		if !ok {
			continue
		}
		job := m.quotaRecoveryJobs[auth.ID]
		if job != nil && (job.running || job.nextCheck.After(now)) {
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			continue
		}
		if job == nil {
			job = &quotaRecoveryJob{}
			if m.quotaRecoveryJobs == nil {
				m.quotaRecoveryJobs = make(map[string]*quotaRecoveryJob)
			}
			m.quotaRecoveryJobs[auth.ID] = job
		}
		job.running = true
		snapshot := auth.Clone()
		go func() {
			var errCheck error
			defer func() {
				m.finishQuotaRecovery(job, errCheck)
				<-slots
			}()
			var recovered bool
			recovered, errCheck = checker.QuotaRecovered(ctx, snapshot)
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

func (m *Manager) finishQuotaRecovery(job *quotaRecoveryJob, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.running = false
	job.delay = min(max(job.delay*2, quotaRecoveryInterval), quotaRecoveryMaxBackoff)
	delay := job.delay
	if retryAfter := retryAfterFromError(err); retryAfter != nil && *retryAfter > delay {
		delay = *retryAfter
	}
	job.nextCheck = time.Now().Add(delay)
}
