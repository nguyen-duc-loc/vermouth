//nolint:testpackage // White box tests prove registry bounds and token accounting.
package ratelimit

import (
	"crypto/sha256"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// covers: AC-2, AC-4
func TestLimiter_AppliesMinuteAndHourBuckets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	config := testConfig(BucketPairConfig{Minute: 5, Hour: 20}, BucketPairConfig{Minute: 100, Hour: 500})
	limiter := New(config, func() time.Time { return now })
	key := testIPKey("caller")

	for range 5 {
		require.True(t, limiter.Check(EndpointStart, key).Allowed)
	}
	minuteRefusal := limiter.Check(EndpointStart, key)
	require.False(t, minuteRefusal.Allowed)
	require.Equal(t, 12*time.Second, minuteRefusal.RetryAfter)
	require.Equal(t, []Scope{ScopeIP}, minuteRefusal.Scopes)
	now = now.Add(100 * time.Millisecond)
	require.Equal(t, 12*time.Second, limiter.Check(EndpointStart, key).RetryAfter)

	now = now.Add(12 * time.Second)
	require.True(t, limiter.Check(EndpointStart, key).Allowed)

	hourConfig := testConfig(BucketPairConfig{Minute: 100, Hour: 2}, BucketPairConfig{Minute: 100, Hour: 500})
	hourLimiter := New(hourConfig, func() time.Time { return now })
	require.True(t, hourLimiter.Check(EndpointStart, key).Allowed)
	require.True(t, hourLimiter.Check(EndpointStart, key).Allowed)
	hourRefusal := hourLimiter.Check(EndpointStart, key)
	require.False(t, hourRefusal.Allowed)
	require.Equal(t, 30*time.Minute, hourRefusal.RetryAfter)
}

// covers: AC-3
func TestLimiter_RefusalConsumesNoOtherBucket(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	config := testConfig(BucketPairConfig{Minute: 1, Hour: 100}, BucketPairConfig{Minute: 2, Hour: 100})
	limiter := New(config, func() time.Time { return now })

	require.True(t, limiter.Check(EndpointStart, testIPKey("first")).Allowed)
	require.False(t, limiter.Check(EndpointStart, testIPKey("first")).Allowed)
	require.True(t, limiter.Check(EndpointStart, testIPKey("second")).Allowed)
}

// covers: AC-3, AC-7
func TestLimiter_RefusalAcrossMultipleScopesConsumesNothing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	config := Config{
		Refresh: EndpointPolicy{
			IP:           BucketPairConfig{Minute: 1, Hour: 100},
			RefreshToken: BucketPairConfig{Minute: 2, Hour: 100},
			Global:       BucketPairConfig{Minute: 2, Hour: 100},
		},
	}
	limiter := New(config, func() time.Time { return now })
	token := Key{Scope: ScopeRefreshToken, Digest: sha256.Sum256([]byte("token"))}

	require.True(t, limiter.Check(EndpointRefresh, testIPKey("first"), token).Allowed)
	callerRefusal := limiter.Check(EndpointRefresh, testIPKey("first"), token)
	require.False(t, callerRefusal.Allowed)
	require.Equal(t, []Scope{ScopeIP}, callerRefusal.Scopes)
	require.True(t, limiter.Check(EndpointRefresh, testIPKey("second"), token).Allowed)

	sharedRefusal := limiter.Check(EndpointRefresh, testIPKey("third"), token)
	require.False(t, sharedRefusal.Allowed)
	require.Equal(t, []Scope{ScopeGlobal, ScopeRefreshToken}, sharedRefusal.Scopes)
	require.Equal(t, 30*time.Second, sharedRefusal.RetryAfter)
	require.Len(t, limiter.callers, 3)
}

// covers: AC-5, AC-8
func TestLimiter_GlobalPressureCreatesNoCallerEntries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	config := testConfig(BucketPairConfig{Minute: 100, Hour: 100}, BucketPairConfig{Minute: 1, Hour: 100})
	limiter := New(config, func() time.Time { return now })
	require.True(t, limiter.Check(EndpointStart, testIPKey("admitted")).Allowed)

	for index := range 10_001 {
		decision := limiter.Check(EndpointStart, testIPKey(fmt.Sprintf("blocked-%d", index)))
		require.False(t, decision.Allowed)
		require.Equal(t, []Scope{ScopeGlobal}, decision.Scopes)
	}
	require.Len(t, limiter.callers, 1)
}

// covers: AC-8
func TestLimiter_EvictsLeastRecentAndExpiresIdleCallers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	large := BucketPairConfig{Minute: 20_000, Hour: 20_000}
	limiter := New(testConfig(large, large), func() time.Time { return now })
	first := testIPKey("first")
	second := testIPKey("second")
	require.True(t, limiter.Check(EndpointStart, first).Allowed)
	require.True(t, limiter.Check(EndpointStart, second).Allowed)
	for index := 2; index < maxCallerEntries; index++ {
		require.True(t, limiter.Check(EndpointStart, testIPKey(fmt.Sprintf("caller-%d", index))).Allowed)
	}
	require.Len(t, limiter.callers, maxCallerEntries)
	require.True(t, limiter.Check(EndpointStart, first).Allowed)
	require.True(t, limiter.Check(EndpointStart, testIPKey("overflow")).Allowed)
	_, firstExists := limiter.callers[registryKey{endpoint: EndpointStart, scope: first.Scope, digest: first.Digest}]
	_, secondExists := limiter.callers[registryKey{endpoint: EndpointStart, scope: second.Scope, digest: second.Digest}]
	require.True(t, firstExists)
	require.False(t, secondExists)

	now = now.Add(callerIdleTTL)
	fresh := testIPKey("fresh")
	require.True(t, limiter.Check(EndpointStart, fresh).Allowed)
	require.Len(t, limiter.callers, 1)
}

// covers: AC-3
func TestLimiter_ClampsBackwardTime(t *testing.T) {
	t.Parallel()

	initial := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	now := initial
	config := testConfig(BucketPairConfig{Minute: 1, Hour: 100}, BucketPairConfig{Minute: 100, Hour: 500})
	limiter := New(config, func() time.Time { return now })
	key := testIPKey("caller")

	require.True(t, limiter.Check(EndpointStart, key).Allowed)
	now = initial.Add(-time.Hour)
	require.False(t, limiter.Check(EndpointStart, key).Allowed)
	now = initial.Add(time.Minute)
	require.True(t, limiter.Check(EndpointStart, key).Allowed)
}

// covers: AC-3
func TestLimiter_ConcurrentChecksCannotOverspend(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	capacity := BucketPairConfig{Minute: 10, Hour: 10}
	limiter := New(testConfig(capacity, capacity), func() time.Time { return now })
	key := testIPKey("caller")
	var admitted atomic.Int64
	start := make(chan struct{})
	done := make(chan struct{}, 100)

	for range 100 {
		go func() {
			<-start
			if limiter.Check(EndpointStart, key).Allowed {
				admitted.Add(1)
			}
			done <- struct{}{}
		}()
	}
	close(start)
	for range 100 {
		<-done
	}

	require.Equal(t, int64(10), admitted.Load())
}

// covers: AC-5, AC-13
func TestPendingAttemptBounds_UsesTheGlobalPolicyAndSweepWindow(t *testing.T) {
	t.Parallel()

	live, physical := pendingAttemptBounds(
		BucketPairConfig{Minute: 100, Hour: 500},
		10*time.Minute,
		10*time.Minute,
	)

	require.Equal(t, 583, live)
	require.Equal(t, 666, physical)
}

func testConfig(caller, global BucketPairConfig) Config {
	return Config{
		Start:    EndpointPolicy{IP: caller, Global: global},
		Callback: EndpointPolicy{IP: caller, Global: global},
		Refresh: EndpointPolicy{
			IP: caller, RefreshToken: caller, Global: global,
		},
	}
}

func testIPKey(value string) Key {
	return Key{Scope: ScopeIP, Digest: sha256.Sum256([]byte(value))}
}
