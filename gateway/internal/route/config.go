// Package route is the gateway's surface: routing, one error shape, and the
// read only aggregation for screens. It holds no business rule and no write
// logic of its own (spec 0001, the service map).
package route

import (
	"os"
	"strings"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
)

// UpstreamsFromEnv reads where the services are. They are required rather than
// defaulted, because a gateway pointing at the wrong place is worse than one
// that refuses to start (STK-8).
func UpstreamsFromEnv() (aggregate.Upstreams, error) {
	upstreams := aggregate.Upstreams{}
	for name, target := range map[string]*string{
		"GATEWAY_IDENTITY_URL":      &upstreams.Identity,
		"GATEWAY_TEACHING_URL":      &upstreams.Teaching,
		"GATEWAY_BILLING_URL":       &upstreams.Billing,
		"GATEWAY_NOTIFICATIONS_URL": &upstreams.Notifications,
	} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			return aggregate.Upstreams{}, &vermouth.MissingEnvError{Name: name}
		}
		*target = strings.TrimSuffix(value, "/")
	}

	return upstreams, nil
}
