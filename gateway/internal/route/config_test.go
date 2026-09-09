package route_test

import (
	"testing"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/route"
)

// covers: AC-8, AC-9, AC-12
func TestUpstreamsFromEnv_LoadsEveryRequiredServiceAddress(t *testing.T) {
	t.Setenv("GATEWAY_IDENTITY_URL", " https://identity.example/ ")
	t.Setenv("GATEWAY_TEACHING_URL", "https://teaching.example/")
	t.Setenv("GATEWAY_BILLING_URL", "https://billing.example/")
	t.Setenv("GATEWAY_NOTIFICATIONS_URL", "https://notifications.example")

	upstreams, err := route.UpstreamsFromEnv()

	require.NoError(t, err)
	require.Equal(t, "https://identity.example", upstreams.Identity)
	require.Equal(t, "https://teaching.example", upstreams.Teaching)
	require.Equal(t, "https://billing.example", upstreams.Billing)
	require.Equal(t, "https://notifications.example", upstreams.Notifications)
}

func TestUpstreamsFromEnv_ReportsTheMissingRequiredAddress(t *testing.T) {
	t.Setenv("GATEWAY_IDENTITY_URL", "https://identity.example")
	t.Setenv("GATEWAY_TEACHING_URL", "https://teaching.example")
	t.Setenv("GATEWAY_BILLING_URL", "   ")
	t.Setenv("GATEWAY_NOTIFICATIONS_URL", "https://notifications.example")

	_, err := route.UpstreamsFromEnv()

	var missing *vermouth.MissingEnvError
	require.ErrorAs(t, err, &missing)
	require.Equal(t, "GATEWAY_BILLING_URL", missing.Name)
}
