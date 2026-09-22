// Package recoverytest isolates recovery regressions in the real test stack.
package recoverytest

import (
	"context"
	urlpkg "net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	cleanupTimeout  = 10 * time.Second
	topicPartitions = 2
)

// Fixture keeps each regression away from service projections and event logs.
type Fixture struct {
	Pool        *pgxpool.Pool
	DatabaseURL string
	Seeds       []string
	Client      *kgo.Client
	Admin       *kadm.Client
	Topic       string
	Group       string
}

// New creates a private schema and two partition topic in the existing stack.
func New(t *testing.T) *Fixture {
	t.Helper()
	databaseURL := os.Getenv("BILLING_DATABASE_URL")
	brokers := os.Getenv("BROKER_SEEDS")
	if databaseURL == "" || brokers == "" {
		t.Skip("BILLING_DATABASE_URL and BROKER_SEEDS are required: run task infra:up, then task test")
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	pool, scopedURL := newDatabase(t, databaseURL, suffix)
	ctx := t.Context()
	seeds := strings.Split(brokers, ",")
	client, err := kgo.NewClient(kgo.SeedBrokers(seeds...), kgo.RecordPartitioner(kgo.ManualPartitioner()))
	require.NoError(t, err)
	t.Cleanup(client.Close)
	admin := kadm.NewClient(client)
	topic := "recovery.test." + suffix
	group := "recovery." + suffix
	retention := "-1"
	created, err := admin.CreateTopics(ctx, topicPartitions, 1, map[string]*string{"retention.ms": &retention}, topic, topic+".dlq")
	require.NoError(t, err)
	require.NoError(t, created.Error())
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), cleanupTimeout)
		defer cancel()
		_, deleteErr := admin.DeleteGroups(cleanupCtx, group)
		require.NoError(t, deleteErr)
		deleted, deleteErr := admin.DeleteTopics(cleanupCtx, topic, topic+".dlq")
		require.NoError(t, deleteErr)
		require.NoError(t, deleted.Error())
	})
	return &Fixture{
		Pool: pool, DatabaseURL: scopedURL, Seeds: seeds, Client: client,
		Admin: admin, Topic: topic, Group: group,
	}
}

func newDatabase(t *testing.T, databaseURL, suffix string) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := t.Context()
	schema := "recovery_test_" + suffix
	conn, err := pgx.Connect(ctx, databaseURL)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), cleanupTimeout)
		defer cancel()
		_, dropErr := conn.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, dropErr)
		require.NoError(t, conn.Close(cleanupCtx))
	})
	parsed, err := urlpkg.Parse(databaseURL)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	pool, err := pgxpool.New(ctx, parsed.String())
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, source, _, ok := runtime.Caller(0)
	require.True(t, ok)
	for _, name := range []string{"00001_vermouth_kit.sql", "00002_consumer_recovery.sql"} {
		ddl, readErr := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "..", "ddl", name)) //nolint:gosec // Only the two checked in canonical DDL files are read.
		require.NoError(t, readErr)
		up, _, _ := strings.Cut(string(ddl), "-- +goose Down")
		_, err = pool.Exec(ctx, up)
		require.NoError(t, err)
	}
	return pool, parsed.String()
}
