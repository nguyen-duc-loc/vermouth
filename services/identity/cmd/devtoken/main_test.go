package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

var errPoolNotReady = errors.New("pool not ready")

func TestOpenPoolWithRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cancel       bool
		wantAttempts int
		wantErr      bool
	}{
		{name: "retries then connects", wantAttempts: 2},
		{name: "stops when canceled", cancel: true, wantAttempts: 0, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			if test.cancel {
				cancel()
			} else {
				t.Cleanup(cancel)
			}
			attempts := 0
			open := func(context.Context, string) (*pgxpool.Pool, error) {
				attempts++
				if !test.cancel && attempts == 2 {
					return new(pgxpool.Pool), nil
				}
				return nil, errPoolNotReady
			}

			_, err := openPoolWithRetry(ctx, "database-url", time.Nanosecond, open)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.wantAttempts, attempts)
		})
	}
}

// covers: AC-13
func TestRunRequiresIdentityDatabaseBeforeLoadingTheSigner(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{service}
	t.Cleanup(func() { os.Args = originalArgs })
	t.Setenv("IDENTITY_DATABASE_URL", "")

	err := run()
	var missing *vermouth.MissingEnvError
	require.ErrorAs(t, err, &missing)
	require.Equal(t, "IDENTITY_DATABASE_URL", missing.Name)
}

// covers: AC-11, AC-13
func TestOpenPoolWithRetryReportsTheLastFailureWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	attempts := 0
	open := func(context.Context, string) (*pgxpool.Pool, error) {
		attempts++
		cancel()
		return nil, errPoolNotReady
	}

	_, err := openPoolWithRetry(ctx, "database-url", time.Hour, open)
	require.ErrorIs(t, err, errPoolNotReady)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}
