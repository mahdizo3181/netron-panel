package panel

import (
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
)

var (
	updateInfoTTL = 10 * time.Minute
	// Unauthenticated GitHub allows 60 calls/h per IP; a failing check waits this
	// long before the next try instead of retrying on every page load.
	updateCheckRetryAfter = time.Minute
)

var updateInfoCache struct {
	mu        sync.Mutex
	info      *PanelUpdateInfo
	err       error
	fetchedAt time.Time
	loggedErr string
}

// cachedUpdateInfo serves fetch's answer for updateInfoTTL; a failed refresh
// keeps the last good answer and is logged once per distinct error.
func cachedUpdateInfo(fetch func() (*PanelUpdateInfo, error)) (*PanelUpdateInfo, error) {
	c := &updateInfoCache
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl := updateInfoTTL
	if c.err != nil {
		ttl = updateCheckRetryAfter
	}
	if c.fetchedAt.IsZero() || time.Since(c.fetchedAt) >= ttl {
		info, err := fetch()
		c.fetchedAt, c.err = time.Now(), err
		if err == nil {
			c.info = info
		}
		logUpdateCheckChange(err)
	}
	if c.info == nil {
		return nil, c.err
	}
	answer := *c.info
	return &answer, nil
}

func logUpdateCheckChange(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	switch {
	case msg == updateInfoCache.loggedErr:
	case err != nil:
		logger.Warning("panel update check failed:", err)
	default:
		logger.Info("panel update check succeeded again")
	}
	updateInfoCache.loggedErr = msg
}

func resetUpdateInfoCache() {
	updateInfoCache.mu.Lock()
	defer updateInfoCache.mu.Unlock()
	updateInfoCache.info, updateInfoCache.err = nil, nil
	updateInfoCache.fetchedAt, updateInfoCache.loggedErr = time.Time{}, ""
}
