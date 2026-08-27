package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDatabaseAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "explicit port", value: "postgres://role:secret@postgres-identity:5432/db", want: "postgres-identity:5432"},
		{name: "default port", value: "postgres://role:secret@postgres-identity/db", want: "postgres-identity:5432"},
		{name: "IPv6 host", value: "postgres://role:secret@[2001:db8::1]:5433/db", want: "[2001:db8::1]:5433"},
		{name: "wrong scheme", value: "http://postgres-identity:5432/db", wantErr: true},
		{name: "missing hostname", value: "postgres:///db", wantErr: true},
		{name: "empty value", value: "", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := databaseAddress(test.value)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestWaitForDatabase_RetriesThenConnects(t *testing.T) {
	t.Parallel()

	attempts := 0
	dial := func(context.Context, string, string) (net.Conn, error) {
		attempts++
		if attempts == 1 {
			return nil, errDatabaseUnavailable
		}
		server, client := net.Pipe()
		t.Cleanup(func() { _ = server.Close() })
		return client, nil
	}

	require.NoError(t, waitForDatabase(t.Context(), "postgres-identity:5432", time.Nanosecond, dial))
	require.Equal(t, 2, attempts)
}

func TestWaitForDatabaseStopsAtTheContextDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	attempts := 0
	dial := func(context.Context, string, string) (net.Conn, error) {
		attempts++
		cancel()
		return nil, errDatabaseUnavailable
	}

	err := waitForDatabase(ctx, "postgres-identity:5432", time.Hour, dial)
	require.ErrorIs(t, err, errDatabaseUnavailable)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, attempts)
}
