package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
)

const sampleInterval = time.Minute

type sampleKey struct {
	endpoint Endpoint
	scope    Scope
}

type sampleState struct {
	last       time.Time
	suppressed int
}

// Guard joins caller derivation, atomic budget checks, and bounded denial logs
// so every limited route uses one policy path.
type Guard struct {
	limiter *Limiter
	logger  *slog.Logger
	clock   func() time.Time

	mu      sync.Mutex
	samples map[sampleKey]sampleState
}

// NewGuard builds the process local auth guard from parsed startup config.
func NewGuard(config Config, logger *slog.Logger, clock func() time.Time) *Guard {
	if clock == nil {
		clock = time.Now
	}
	return &Guard{
		limiter: New(config, clock),
		logger:  logger,
		clock:   clock,
		samples: make(map[sampleKey]sampleState),
	}
}

// Check applies the fixed endpoint policy to a request and any additional
// caller key, then samples a warning for each limiting scope.
func (g *Guard) Check(request *http.Request, endpoint Endpoint, additional ...Key) Decision {
	keys := make([]Key, 0, len(additional)+1)
	keys = append(keys, ClientIPKey(request, g.limiter.config.TrustedProxyCIDRs))
	keys = append(keys, additional...)
	decision := g.limiter.Check(endpoint, keys...)
	if decision.Allowed {
		return decision
	}
	for _, scope := range decision.Scopes {
		g.logDenial(request.Context(), endpoint, scope)
	}
	return decision
}

func (g *Guard) logDenial(ctx context.Context, endpoint Endpoint, scope Scope) {
	now := g.clock()
	key := sampleKey{endpoint: endpoint, scope: scope}

	g.mu.Lock()
	state, exists := g.samples[key]
	if exists && now.Sub(state.last) < sampleInterval {
		state.suppressed++
		g.samples[key] = state
		g.mu.Unlock()
		return
	}
	suppressed := state.suppressed
	g.samples[key] = sampleState{last: now}
	g.mu.Unlock()

	g.logger.WarnContext(ctx, "Authentication request rate limited",
		slog.String("endpoint", string(endpoint)),
		slog.String("scope", string(scope)),
		slog.Int("count", suppressed+1),
		slog.Int("suppressed", suppressed),
		slog.String("request_id", vermouth.RequestID(ctx)),
	)
}
