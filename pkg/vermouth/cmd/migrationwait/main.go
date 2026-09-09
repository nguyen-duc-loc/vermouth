// Command migrationwait waits for a migration database path, then replaces itself with Goose.
//
//nolint:err113,gosec,noinlineerr // The one shot boundary needs precise config errors and execs one fixed absolute Goose path.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	urlpkg "net/url"
	"os"
	"syscall"
	"time"
)

const (
	databaseWait       = 30 * time.Second
	databaseDial       = 2 * time.Second
	databaseRetryDelay = time.Second
)

var errDatabaseUnavailable = errors.New("database did not accept connections before the migration deadline")

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrationwait: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	address, err := databaseAddress(os.Getenv("GOOSE_DBSTRING"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), databaseWait)
	defer cancel()
	dialer := &net.Dialer{Timeout: databaseDial}
	err = waitForDatabase(ctx, address, databaseRetryDelay, dialer.DialContext)
	if err != nil {
		return err
	}
	err = syscall.Exec(
		"/goose",
		[]string{"/goose", "-env", "none", "up"},
		os.Environ(),
	)
	return fmt.Errorf("exec Goose: %w", err)
}

func databaseAddress(value string) (string, error) {
	databaseURL, err := urlpkg.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse GOOSE_DBSTRING: %w", err)
	}
	if databaseURL.Scheme != "postgres" || databaseURL.Hostname() == "" {
		return "", errors.New("GOOSE_DBSTRING must be a Postgres URL with a hostname")
	}
	port := databaseURL.Port()
	if port == "" {
		port = "5432"
	}
	return net.JoinHostPort(databaseURL.Hostname(), port), nil
}

func waitForDatabase(
	ctx context.Context,
	address string,
	retryDelay time.Duration,
	dial func(context.Context, string, string) (net.Conn, error),
) error {
	for {
		connection, err := dial(ctx, "tcp", address)
		if err == nil {
			closeErr := connection.Close()
			if closeErr != nil {
				return fmt.Errorf("close database readiness connection: %w", closeErr)
			}
			return nil
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %w", errDatabaseUnavailable, ctx.Err())
		case <-timer.C:
		}
	}
}
