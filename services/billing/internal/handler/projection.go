package handler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
)

// TeachingProjectionStatus reports diagnostic progress for one tutor. It is
// never used as teaching truth or as an invoice input.
type TeachingProjectionStatus struct {
	State           string     `json:"state"`
	ClassCount      int64      `json:"class_count"`
	SessionCount    int64      `json:"session_count"`
	StudentCount    int64      `json:"student_count"`
	OpenRosterCount int64      `json:"open_roster_count"`
	AttendanceCount int64      `json:"attendance_count"`
	LatestUpdatedAt *time.Time `json:"latest_updated_at"`
}

// ProjectionReader exposes billing owned projection progress without granting
// any caller a path to authoritative invoice records.
type ProjectionReader struct {
	pool *pgxpool.Pool
}

// NewProjectionReader builds the narrow read used by the home panel.
func NewProjectionReader(pool *pgxpool.Pool) *ProjectionReader {
	return &ProjectionReader{pool: pool}
}

// TeachingStatus counts the five first thread projections and becomes active
// only when every count is greater than zero.
func (r *ProjectionReader) TeachingStatus(
	ctx context.Context,
	tutorID uuid.UUID,
) (TeachingProjectionStatus, error) {
	row, err := store.Queries(r.pool).GetTeachingProjectionStatus(ctx, tutorID)
	if err != nil {
		return TeachingProjectionStatus{}, fmt.Errorf("read teaching projection status: %w", err)
	}
	state := "waiting"
	complete := row.ClassCount > 0 && row.SessionCount > 0 && row.StudentCount > 0 &&
		row.OpenRosterCount > 0 && row.AttendanceCount > 0
	if complete {
		state = "active"
	}
	var latestUpdatedAt *time.Time
	if row.HasUpdatedAt {
		value := row.LatestUpdatedAt
		latestUpdatedAt = &value
	}
	return TeachingProjectionStatus{
		State:           state,
		ClassCount:      row.ClassCount,
		SessionCount:    row.SessionCount,
		StudentCount:    row.StudentCount,
		OpenRosterCount: row.OpenRosterCount,
		AttendanceCount: row.AttendanceCount,
		LatestUpdatedAt: latestUpdatedAt,
	}, nil
}
