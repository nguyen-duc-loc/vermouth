// Command devtoken creates a tutor and signs a token for it, for development
// and for test/thread.sh.
//
// It exists because sign in now needs Google, an internet connection and a real
// OAuth client, so `task thread` would otherwise need a browser (AC-13). It is
// deliberately a separate main package that no service binary imports: a way to
// mint a token without Google has to stay outside anything that ships, and the
// list of Mint callers is the checkable form of that.
//
// It writes the tutor and its identity.tutor.registered event in one
// transaction, the same way the callback does, because the point of the thread
// is the event reaching notifications.
package main

// The blank time/tzdata import embeds the zone database, the same reason every
// other main here does it (STK-6).
import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store/sqlcgen"
	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/token"
)

const (
	service           = "devtoken"
	databaseWait      = 30 * time.Second
	databaseRetryWait = time.Second
)

type poolOpener func(context.Context, string) (*pgxpool.Pool, error)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
		os.Exit(1)
	}
}

// tutorRegistered is the field list spec 0001's catalogue names for
// identity.tutor.registered. It is declared here rather than shared with the
// service, so this program stays outside identity's code rather than becoming a
// door inside it (INV-12).
type tutorRegistered struct {
	TutorID     uuid.UUID `json:"tutor_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Timezone    string    `json:"timezone"`
	Language    string    `json:"language"`
}

// answer is what this program prints, shaped like the session the browser gets
// so test/thread.sh reads one JSON object either way.
type answer struct {
	SchemaVersion   int       `json:"schema_version"`
	TutorID         uuid.UUID `json:"tutor_id"`
	Email           string    `json:"email"`
	AccessToken     string    `json:"access_token"`
	AccessExpiresAt time.Time `json:"access_expires_at"`
}

func run() error {
	// Its own flag set rather than the package one, so parsing stays inside the
	// function that uses it.
	flags := flag.NewFlagSet(service, flag.ContinueOnError)
	email := flags.String("email", "", "the tutor's email, or empty for a fresh one")
	name := flags.String("name", "Tracer Tutor", "the tutor's display name")
	timezone := flags.String("timezone", "Asia/Ho_Chi_Minh", "an IANA timezone name")
	language := flags.String("language", "vi", "vi or en")
	err := flags.Parse(os.Args[1:])
	if err != nil {
		return fmt.Errorf("read the flags: %w", err)
	}

	if *email == "" {
		*email = fmt.Sprintf("tutor-%d@example.com", time.Now().UnixNano())
	}
	databaseURL := os.Getenv("IDENTITY_DATABASE_URL")
	if databaseURL == "" {
		return &vermouth.MissingEnvError{Name: "IDENTITY_DATABASE_URL", Reason: ""}
	}
	signer, err := token.NewSignerFromEnv()
	if err != nil {
		return err
	}

	ctx := context.Background()
	waitCtx, cancel := context.WithTimeout(ctx, databaseWait)
	pool, err := openPoolWithRetry(waitCtx, databaseURL, databaseRetryWait, vermouth.OpenPool)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()

	tutorID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("new tutor id: %w", err)
	}
	row, err := insertTutor(ctx, pool, sqlcgen.InsertTutorParams{
		TutorID:     tutorID,
		Email:       *email,
		DisplayName: *name,
		Timezone:    *timezone,
		Language:    *language,
	})
	if err != nil {
		return err
	}

	accessToken, expiresAt, err := signer.Mint(row.TutorID, row.Timezone, row.Language)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	//nolint:gosec // G117: printing the token is this program's whole purpose, and it never leaves a development machine.
	return encoder.Encode(answer{
		SchemaVersion:   1,
		TutorID:         row.TutorID,
		Email:           row.Email,
		AccessToken:     accessToken,
		AccessExpiresAt: expiresAt,
	})
}

func openPoolWithRetry(
	ctx context.Context,
	databaseURL string,
	retryDelay time.Duration,
	open poolOpener,
) (*pgxpool.Pool, error) {
	var lastErr error
	for {
		ctxErr := ctx.Err()
		if ctxErr != nil {
			return nil, fmt.Errorf("wait for identity database: %w", errors.Join(lastErr, ctxErr))
		}
		pool, err := open(ctx, databaseURL)
		if err == nil {
			return pool, nil
		}
		lastErr = err
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("wait for identity database: %w", errors.Join(lastErr, ctx.Err()))
		case <-timer.C:
		}
	}
}

// insertTutor writes the tutor and its event in one transaction, which is the
// shape every write in this repository takes (INV-3, STK-4).
func insertTutor(
	ctx context.Context, pool *pgxpool.Pool, params sqlcgen.InsertTutorParams,
) (sqlcgen.InsertTutorRow, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return sqlcgen.InsertTutorRow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row, err := store.Queries(tx).InsertTutor(ctx, params)
	if err != nil {
		return sqlcgen.InsertTutorRow{}, fmt.Errorf("insert tutor: %w", err)
	}
	env, err := vermouth.NewEnvelope(ctx, vermouth.EventTutorRegistered, 1, row.TutorID,
		vermouth.Key{Kind: vermouth.KeyTutorID, Value: row.TutorID},
		tutorRegistered{
			TutorID:     row.TutorID,
			Email:       row.Email,
			DisplayName: row.DisplayName,
			Timezone:    row.Timezone,
			Language:    row.Language,
		})
	if err != nil {
		return sqlcgen.InsertTutorRow{}, err
	}
	err = vermouth.WriteOutbox(ctx, tx, vermouth.TopicIdentity, env)
	if err != nil {
		return sqlcgen.InsertTutorRow{}, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return sqlcgen.InsertTutorRow{}, fmt.Errorf("commit tutor and its event: %w", err)
	}
	return row, nil
}
