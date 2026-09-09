package ratelimit

import (
	"container/list"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	maxCallerEntries  = 10_000
	callerIdleTTL     = 2 * time.Hour
	cleanupInterval   = time.Minute
	authEndpointCount = 3
)

// Decision says whether the request may cross the gateway boundary. Scopes
// contains only fixed values and is safe for sampled structured logging.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
	Scopes     []Scope
}

type bucketPair struct {
	minute     *rate.Limiter
	hour       *rate.Limiter
	minuteRate rate.Limit
	hourRate   rate.Limit
}

type registryKey struct {
	endpoint Endpoint
	scope    Scope
	digest   [32]byte
}

type callerEntry struct {
	buckets  bucketPair
	lastSeen time.Time
	position *list.Element
}

type lruValue struct {
	key registryKey
}

type scopedPair struct {
	scope   Scope
	buckets *bucketPair
}

// Limiter owns all global and caller token buckets for one gateway process.
// One mutex makes the multi bucket check and spend atomic.
type Limiter struct {
	config Config
	clock  func() time.Time

	mu          sync.Mutex
	callers     map[registryKey]*callerEntry
	recent      list.List
	globals     map[Endpoint]bucketPair
	lastNow     time.Time
	lastCleanup time.Time
}

// New builds full global buckets and an empty bounded caller registry.
func New(config Config, clock func() time.Time) *Limiter {
	if clock == nil {
		clock = time.Now
	}
	limiter := &Limiter{
		config:  config,
		clock:   clock,
		callers: make(map[registryKey]*callerEntry, maxCallerEntries),
		globals: make(map[Endpoint]bucketPair, authEndpointCount),
	}
	for _, endpoint := range []Endpoint{EndpointStart, EndpointCallback, EndpointRefresh} {
		policy := config.policy(endpoint)
		limiter.globals[endpoint] = newBucketPair(policy.Global)
	}
	return limiter
}

// Check observes and, only when all applicable buckets allow it, spends one
// token from every bucket at the same instant.
func (l *Limiter) Check(endpoint Endpoint, keys ...Key) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clampedNow()
	l.cleanupIdle(now)
	global, exists := l.globals[endpoint]
	if !exists {
		return Decision{RetryAfter: time.Second, Scopes: []Scope{ScopeGlobal}}
	}

	pairs := []scopedPair{{scope: ScopeGlobal, buckets: &global}}
	if !pairAvailable(global, now) {
		for _, key := range uniqueKeys(keys) {
			entry := l.callers[registryKey{endpoint: endpoint, scope: key.Scope, digest: key.Digest}]
			if entry != nil {
				pairs = append(pairs, scopedPair{scope: key.Scope, buckets: &entry.buckets})
			}
		}
		return inspect(pairs, now)
	}

	policy := l.config.policy(endpoint)
	for _, key := range uniqueKeys(keys) {
		entryKey := registryKey{endpoint: endpoint, scope: key.Scope, digest: key.Digest}
		entry := l.callers[entryKey]
		if entry == nil {
			entry = l.insertCaller(entryKey, policyForScope(policy, key.Scope), now)
		} else {
			entry.lastSeen = now
			l.recent.MoveToFront(entry.position)
		}
		pairs = append(pairs, scopedPair{scope: key.Scope, buckets: &entry.buckets})
	}

	decision := inspect(pairs, now)
	if decision.Allowed {
		for _, pair := range pairs {
			pair.buckets.minute.AllowN(now, 1)
			pair.buckets.hour.AllowN(now, 1)
		}
		l.globals[endpoint] = global
	}
	return decision
}

func inspect(pairs []scopedPair, now time.Time) Decision {
	decision := Decision{Allowed: true}
	for _, pair := range pairs {
		available, retryAfter := pairStatus(*pair.buckets, now)
		if available {
			continue
		}
		decision.Allowed = false
		decision.Scopes = append(decision.Scopes, pair.scope)
		if retryAfter > decision.RetryAfter {
			decision.RetryAfter = retryAfter
		}
	}
	return decision
}

func newBucketPair(config BucketPairConfig) bucketPair {
	minuteRate := rate.Limit(float64(config.Minute) / time.Minute.Seconds())
	hourRate := rate.Limit(float64(config.Hour) / time.Hour.Seconds())
	return bucketPair{
		minute:     rate.NewLimiter(minuteRate, config.Minute),
		hour:       rate.NewLimiter(hourRate, config.Hour),
		minuteRate: minuteRate,
		hourRate:   hourRate,
	}
}

func pairAvailable(pair bucketPair, now time.Time) bool {
	available, _ := pairStatus(pair, now)
	return available
}

func pairStatus(pair bucketPair, now time.Time) (bool, time.Duration) {
	minuteTokens := pair.minute.TokensAt(now)
	hourTokens := pair.hour.TokensAt(now)
	available := minuteTokens >= 1 && hourTokens >= 1
	return available, maxDuration(
		retryDelay(minuteTokens, pair.minuteRate),
		retryDelay(hourTokens, pair.hourRate),
	)
}

func retryDelay(tokens float64, refill rate.Limit) time.Duration {
	if tokens >= 1 {
		return 0
	}
	seconds := (1 - tokens) / float64(refill)
	delay := time.Duration(math.Ceil(seconds)) * time.Second
	if delay < time.Second {
		return time.Second
	}
	return delay
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}

func pendingAttemptBounds(
	global BucketPairConfig,
	lifetime time.Duration,
	sweepInterval time.Duration,
) (live int, physical int) {
	return admittedRequestBound(global, lifetime), admittedRequestBound(global, lifetime+sweepInterval)
}

func admittedRequestBound(config BucketPairConfig, duration time.Duration) int {
	minute := config.Minute + int(float64(config.Minute)*duration.Minutes())
	hour := config.Hour + int(float64(config.Hour)*duration.Hours())
	if minute < hour {
		return minute
	}
	return hour
}

func policyForScope(policy EndpointPolicy, scope Scope) BucketPairConfig {
	switch scope {
	case ScopeIP:
		return policy.IP
	case ScopeRefreshToken:
		return policy.RefreshToken
	case ScopeGlobal:
		return BucketPairConfig{Minute: 1, Hour: 1}
	default:
		return BucketPairConfig{}
	}
}

func uniqueKeys(keys []Key) []Key {
	seen := make(map[Key]struct{}, len(keys))
	unique := make([]Key, 0, len(keys))
	for _, key := range keys {
		if key.Scope != ScopeIP && key.Scope != ScopeRefreshToken {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}
	return unique
}

func (l *Limiter) insertCaller(key registryKey, config BucketPairConfig, now time.Time) *callerEntry {
	if len(l.callers) == maxCallerEntries {
		oldest := l.recent.Back()
		if oldest != nil {
			l.removeCaller(oldest.Value.(lruValue).key)
		}
	}
	position := l.recent.PushFront(lruValue{key: key})
	entry := &callerEntry{buckets: newBucketPair(config), lastSeen: now, position: position}
	l.callers[key] = entry
	return entry
}

func (l *Limiter) cleanupIdle(now time.Time) {
	if !l.lastCleanup.IsZero() && now.Sub(l.lastCleanup) < cleanupInterval {
		return
	}
	l.lastCleanup = now
	for key, entry := range l.callers {
		if now.Sub(entry.lastSeen) >= callerIdleTTL {
			l.removeCaller(key)
		}
	}
}

func (l *Limiter) removeCaller(key registryKey) {
	entry := l.callers[key]
	if entry == nil {
		return
	}
	l.recent.Remove(entry.position)
	delete(l.callers, key)
}

func (l *Limiter) clampedNow() time.Time {
	now := l.clock()
	if !l.lastNow.IsZero() && now.Before(l.lastNow) {
		return l.lastNow
	}
	l.lastNow = now
	return now
}
