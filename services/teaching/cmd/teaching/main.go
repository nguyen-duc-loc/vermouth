// Command teaching is the service that owns what is taught: classes,
// schedules, sessions, students, roster membership, attendance and the current
// tuition rate on a class (spec 0001).
//
// It holds a copy of nothing: the identity facts it needs, including the
// timezone it computes a session's local_date from, arrive in the token.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"golang.org/x/sync/errgroup"

	teachinghttp "github.com/nguyen-duc-loc/vermouth/services/teaching/internal/http"
)

const service = "teaching"

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := vermouth.LoadConfig(service, vermouth.TopicTeaching)
	if err != nil {
		return err
	}
	logger := vermouth.NewLogger(cfg.Service, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := vermouth.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	err = vermouth.EnsureTopics(ctx, cfg.BrokerSeeds, cfg.TopicPartitions, cfg.PublishTopic)
	if err != nil {
		return err
	}

	producer, err := vermouth.NewProducer(cfg.BrokerSeeds, cfg.Service)
	if err != nil {
		return err
	}
	defer producer.Close()

	health := vermouth.Health{
		Service: cfg.Service,
		Logger:  logger,
		Checks: map[string]vermouth.Check{
			"database": func(ctx context.Context) error { return pool.Ping(ctx) },
			"broker":   func(ctx context.Context) error { return producer.Ping(ctx) },
		},
	}
	mux := teachinghttp.Mux(teachinghttp.Deps{Logger: logger, Health: health})

	// The relay runs from the first day, so the first feature to write an
	// event has nothing to wire (STK-18).
	relay := vermouth.NewRelay(pool, producer, logger, cfg.RelayInterval)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return vermouth.Serve(groupCtx, logger, cfg.HTTPAddr, mux) })
	group.Go(func() error { return relay.Run(groupCtx) })
	err = group.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
