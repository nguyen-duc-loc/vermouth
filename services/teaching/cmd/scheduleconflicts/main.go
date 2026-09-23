// Command scheduleconflicts reports active teaching session overlaps without
// changing them, so an operator can reconcile data before the guarded migration.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store"
)

const databaseEnvironment = "TEACHING_DATABASE_URL"

type conflict struct {
	TutorID         uuid.UUID `json:"tutor_id"`
	FirstSessionID  uuid.UUID `json:"first_session_id"`
	FirstClassID    uuid.UUID `json:"first_class_id"`
	FirstStartsAt   time.Time `json:"first_starts_at"`
	FirstEndsAt     time.Time `json:"first_ends_at"`
	SecondSessionID uuid.UUID `json:"second_session_id"`
	SecondClassID   uuid.UUID `json:"second_class_id"`
	SecondStartsAt  time.Time `json:"second_starts_at"`
	SecondEndsAt    time.Time `json:"second_ends_at"`
}

func main() {
	err := run(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "schedule conflicts: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	databaseURL := os.Getenv(databaseEnvironment)
	if databaseURL == "" {
		return &vermouth.MissingEnvError{Name: databaseEnvironment}
	}
	pool, err := vermouth.OpenPool(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	rows, err := store.Queries(pool).ListSessionConflicts(ctx)
	if err != nil {
		return fmt.Errorf("query conflicts: %w", err)
	}
	result := make([]conflict, 0, len(rows))
	for index := range rows {
		row := &rows[index]
		result = append(result, conflict{
			TutorID:         row.TutorID,
			FirstSessionID:  row.FirstSessionID,
			FirstClassID:    row.FirstClassID,
			FirstStartsAt:   row.FirstStartsAt,
			FirstEndsAt:     row.FirstEndsAt,
			SecondSessionID: row.SecondSessionID,
			SecondClassID:   row.SecondClassID,
			SecondStartsAt:  row.SecondStartsAt,
			SecondEndsAt:    row.SecondEndsAt,
		})
	}
	err = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		return fmt.Errorf("encode conflicts: %w", err)
	}
	return nil
}
