// Package route is the gateway's surface: routing, one error shape, and the
// read only aggregation for screens. It holds no business rule and no write
// logic of its own (spec 0001, the service map).
package route

import (
	"os"
	"strings"
	"time"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
)

// defaultUpstreamTimeout is how long one service call may take before the
// gateway gives up on it, unless GATEWAY_UPSTREAM_TIMEOUT says otherwise.
const defaultUpstreamTimeout = 5 * time.Second

// UpstreamsFromEnv reads where the services are. They are required rather than
// defaulted, because a gateway pointing at the wrong place is worse than one
// that refuses to start (STK-8).
func UpstreamsFromEnv() (aggregate.Upstreams, time.Duration, error) {
	upstreams := aggregate.Upstreams{}
	for name, target := range map[string]*string{
		"GATEWAY_IDENTITY_URL":      &upstreams.Identity,
		"GATEWAY_TEACHING_URL":      &upstreams.Teaching,
		"GATEWAY_BILLING_URL":       &upstreams.Billing,
		"GATEWAY_NOTIFICATIONS_URL": &upstreams.Notifications,
	} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			return aggregate.Upstreams{}, 0, &vermouth.MissingEnvError{Name: name}
		}
		*target = strings.TrimSuffix(value, "/")
	}

	timeout := defaultUpstreamTimeout
	if raw := strings.TrimSpace(os.Getenv("GATEWAY_UPSTREAM_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return aggregate.Upstreams{}, 0, &vermouth.MissingEnvError{Name: "GATEWAY_UPSTREAM_TIMEOUT", Reason: "not a duration such as 5s"}
		}
		timeout = parsed
	}
	return upstreams, timeout, nil
}
