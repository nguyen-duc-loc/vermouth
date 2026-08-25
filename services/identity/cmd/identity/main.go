// Command identity is the service that owns proof of who you are: tutor_id,
// email, the linked Google account, display name, timezone and language (spec
// 0001). It stores no password (spec 0004).
package main

// The blank time/tzdata import below embeds the zone database, because a FROM
// scratch image carries no system tzdata while a timezone name still has to
// resolve (STK-6).
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/sync/errgroup"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/handler"
	identityhttp "github.com/nguyen-duc-loc/vermouth/services/identity/internal/http"
	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/token"
)

const service = "identity"

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
		os.Exit(1)
	}
}

// keys reads both halves this service holds: the private one it signs with,
// which lives only here (INV-14), and the public set it verifies with.
func keys(cfg vermouth.Config) (*token.Signer, *vermouth.Verifier, error) {
	signer, err := token.NewSignerFromEnv()
	if err != nil {
		return nil, nil, err
	}
	verifier, err := vermouth.NewVerifier(cfg.PublicKeys)
	if err != nil {
		return nil, nil, err
	}
	return signer, verifier, nil
}

// openProducer creates this service's publish topic and that topic's dead letter
// topic before the relay starts (STK-16). Automatic creation is off on the
// broker, so a forgotten call fails at startup rather than silently later.
func openProducer(ctx context.Context, cfg vermouth.Config) (*kgo.Client, error) {
	err := vermouth.EnsureTopics(ctx, cfg.BrokerSeeds, cfg.TopicPartitions, cfg.PublishTopic)
	if err != nil {
		return nil, err
	}
	return vermouth.NewProducer(cfg.BrokerSeeds, cfg.Service)
}

// health is what /health and /ready answer from: this service's own database and
// its own producer, and nothing another service owns.
func health(
	cfg vermouth.Config, logger *slog.Logger, pool *pgxpool.Pool, producer *kgo.Client,
) vermouth.Health {
	return vermouth.Health{
		Service: cfg.Service,
		Logger:  logger,
		Checks: map[string]vermouth.Check{
			"database": func(ctx context.Context) error { return pool.Ping(ctx) },
			"broker":   func(ctx context.Context) error { return producer.Ping(ctx) },
		},
	}
}

func run() error {
	cfg, err := vermouth.LoadConfig(service, vermouth.TopicIdentity)
	if err != nil {
		return err
	}
	logger := vermouth.NewLogger(cfg.Service, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	signer, verifier, err := keys(cfg)
	if err != nil {
		return err
	}
	// The selected auth mode is read once here, so a missing value stops startup
	// rather than a later sign in (STK-8).
	auth, err := handler.AuthConfigFromEnv()
	if err != nil {
		return err
	}

	pool, err := vermouth.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	producer, err := openProducer(ctx, cfg)
	if err != nil {
		return err
	}
	defer producer.Close()

	relay := vermouth.NewRelay(pool, producer, logger, cfg.RelayInterval)

	identityHandler := handler.New(pool, logger, signer, cfg.PublishTopic, auth)
	logger.Info("Starting", slog.String("kid", signer.KID()), slog.String("topic", cfg.PublishTopic))

	mux := identityhttp.Mux(identityhttp.Deps{
		Handler:  identityHandler,
		Verifier: verifier,
		Signer:   signer,
		Logger:   logger,
		Auth:     auth,
		Health:   health(cfg, logger, pool, producer),
	})

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return vermouth.Serve(groupCtx, logger, cfg.HTTPAddr, mux) })
	group.Go(func() error { return relay.Run(groupCtx) })
	// The sweep is the third goroutine in this binary: expired sign in attempts
	// and finished refresh tokens, on their own interval rather than the relay's.
	group.Go(func() error { return identityHandler.RunSweep(groupCtx) })
	err = group.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
