package deploy

import (
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/cache"
	"sync"
	"time"
)

const (
	mandatoryReconcileTime = 1 * time.Minute
)

type cacheItem struct {
	ts           time.Time
	needCallback bool
}

type CoolDownTracker interface {
	IsCoolingDown(string) (bool, bool)
	Mark(string)
}

type coolDownTrackerImpl struct {
	logger             logr.Logger
	coolDownCache      *cache.Expiring
	coolDownCacheMutex sync.Mutex
	coolDown           time.Duration
}

func (c *coolDownTrackerImpl) IsCoolingDown(name string) (bool, bool) {
	c.coolDownCacheMutex.Lock()
	defer c.coolDownCacheMutex.Unlock()

	item, exists := c.coolDownCache.Get(name)
	c.logger.Info("IsCoolingDown", "name", name)

	if !exists {
		c.coolDownCache.Set(name, cacheItem{
			ts:           time.Now(),
			needCallback: false,
		}, c.coolDown)
		c.logger.Info("Not exists, returning a callback.")
		return true, true
	}

	castItem := item.(cacheItem)
	duration := time.Now().Sub(castItem.ts)
	if castItem.needCallback {
		c.coolDownCache.Set(name, cacheItem{
			ts:           castItem.ts,
			needCallback: false,
		}, c.coolDown)
	}
	c.logger.Info("past not exists check", "name", name, "duration", duration, "needCallback", castItem.needCallback)
	return duration < mandatoryReconcileTime, castItem.needCallback
}

func (c *coolDownTrackerImpl) Mark(name string) {
	c.coolDownCacheMutex.Lock()
	defer c.coolDownCacheMutex.Unlock()

	c.logger.Info("Marking", "name", name)
	c.coolDownCache.Set(name, cacheItem{
		ts:           time.Now(),
		needCallback: true,
	}, c.coolDown)
}

func NewCoolDownTracker(logger logr.Logger) CoolDownTracker {
	return &coolDownTrackerImpl{
		logger:        logger,
		coolDownCache: cache.NewExpiring(),
		coolDown:      time.Minute * 60,
	}
}
