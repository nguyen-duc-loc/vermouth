//nolint:testpackage // These integration tests inject the handler clock and inspect private recovery details.
package handler

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

// covers: AC-1, AC-2, AC-3, AC-4, AC-5, AC-15, AC-20
func TestStudentRecords_OptimisticEditArchiveAndPrivacy(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	commandTime := time.Date(2026, time.August, 30, 4, 0, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, commandTime)
	ctx := vermouth.WithRequestID(t.Context(), "request-student-records")

	createdClass, _, err := work.CreateClass(
		ctx, tutorID, "Asia/Ho_Chi_Minh", "student-records-class", testClassInput(),
	)
	require.NoError(t, err)
	phone := " 090-123 456 "
	student, _, err := work.CreateStudent(
		ctx, tutorID, "student-records-create", CreateStudentInput{Name: " Mai ", Phone: &phone},
	)
	require.NoError(t, err)
	require.Equal(t, "Mai", student.Name)
	require.Equal(t, "090-123 456", *student.Phone)
	require.Equal(t, time.UTC, student.CreatedAt.Location())
	require.Equal(t, time.UTC, student.UpdatedAt.Location())

	page, err := work.ListStudents(ctx, tutorID, "Asia/Ho_Chi_Minh", "mai", "")
	require.NoError(t, err)
	require.Len(t, page.Students, 1)
	require.Equal(t, int64(0), page.Students[0].ActiveClassCount)
	require.Equal(t, time.UTC, page.Students[0].UpdatedAt.Location())

	strangerID := uuid.Must(uuid.NewV7())
	_, err = work.ReadStudent(ctx, strangerID, student.StudentID, "Asia/Ho_Chi_Minh")
	require.ErrorIs(t, err, ErrNotFound)

	updatedName := "Mai Anh"
	updated, err := work.UpdateStudent(
		ctx,
		tutorID,
		student.StudentID,
		"student-records-update",
		PatchStudentInput{
			ExpectedUpdatedAt: student.UpdatedAt.Format(time.RFC3339Nano),
			Name:              &updatedName,
			NameSet:           true,
		},
	)
	require.NoError(t, err)
	require.Equal(t, updatedName, updated.Name)
	require.True(t, updated.UpdatedAt.After(student.UpdatedAt))

	_, err = work.UpdateStudent(
		ctx,
		tutorID,
		student.StudentID,
		"student-records-stale",
		PatchStudentInput{
			ExpectedUpdatedAt: student.UpdatedAt.Format(time.RFC3339Nano),
			Name:              &student.Name,
			NameSet:           true,
		},
	)
	var changed *ConflictError
	require.ErrorAs(t, err, &changed)
	require.Equal(t, "student_changed", changed.Code)
	require.IsType(t, StudentChangedDetails{}, changed.Details)
	details := changed.Details.(StudentChangedDetails)
	require.Equal(t, time.UTC, details.UpdatedAt.Location())

	_, err = work.ChangeClassRoster(
		ctx,
		tutorID,
		createdClass.Class.ClassID,
		"Asia/Ho_Chi_Minh",
		"student-records-roster-add",
		ChangeRosterInput{
			ChangeDate: "2026-08-30",
			Additions:  []uuid.UUID{student.StudentID},
			Removals:   []uuid.UUID{},
		},
	)
	require.NoError(t, err)

	err = work.ArchiveStudent(
		ctx, tutorID, student.StudentID, "Asia/Ho_Chi_Minh", "student-records-archive-blocked",
	)
	var memberships *ConflictError
	require.ErrorAs(t, err, &memberships)
	require.Equal(t, "active_memberships", memberships.Code)

	work.now = func() time.Time { return commandTime.AddDate(0, 0, 1) }
	_, err = work.ChangeClassRoster(
		ctx,
		tutorID,
		createdClass.Class.ClassID,
		"Asia/Ho_Chi_Minh",
		"student-records-roster-remove",
		ChangeRosterInput{
			ChangeDate: "2026-08-31",
			Additions:  []uuid.UUID{},
			Removals:   []uuid.UUID{student.StudentID},
		},
	)
	require.NoError(t, err)
	require.NoError(t, work.ArchiveStudent(
		ctx, tutorID, student.StudentID, "Asia/Ho_Chi_Minh", "student-records-archive",
	))
	_, err = work.ReadStudent(ctx, tutorID, student.StudentID, "Asia/Ho_Chi_Minh")
	require.ErrorIs(t, err, ErrNotFound)

	var leakedPhone bool
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT EXISTS (
			SELECT 1
			FROM outbox
			WHERE tutor_id = $1
			  AND envelope::text LIKE '%' || $2 || '%'
		)`, tutorID, "090-123 456").Scan(&leakedPhone))
	require.False(t, leakedPhone)
}

// covers: AC-11, AC-12, AC-13, AC-15, AC-20
func TestAttendanceSave_StaleRevisionWritesNothing(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	commandTime := time.Date(2026, time.August, 30, 3, 30, 0, 0, time.UTC)
	work := newTeachingHandler(t, pool, commandTime)
	ctx := vermouth.WithRequestID(t.Context(), "request-attendance-stale")

	createdClass, _, err := work.CreateClass(
		ctx, tutorID, "Asia/Ho_Chi_Minh", "attendance-stale-class", testClassInput(),
	)
	require.NoError(t, err)
	student, _, err := work.CreateStudent(
		ctx, tutorID, "attendance-stale-student", CreateStudentInput{Name: "Mai"},
	)
	require.NoError(t, err)
	_, err = work.ChangeClassRoster(
		ctx,
		tutorID,
		createdClass.Class.ClassID,
		"Asia/Ho_Chi_Minh",
		"attendance-stale-roster",
		ChangeRosterInput{
			ChangeDate: "2026-08-30",
			Additions:  []uuid.UUID{student.StudentID},
			Removals:   []uuid.UUID{},
		},
	)
	require.NoError(t, err)

	sheet, err := work.ReadAttendance(ctx, tutorID, createdClass.FirstSession.SessionID)
	require.NoError(t, err)
	_, err = work.SaveAttendance(
		ctx,
		tutorID,
		createdClass.FirstSession.SessionID,
		"attendance-stale-first",
		SaveAttendanceInput{
			Revision: sheet.Revision,
			Marks:    []AttendanceMarkInput{{StudentID: student.StudentID, State: "Present"}},
		},
	)
	require.NoError(t, err)

	_, err = work.SaveAttendance(
		ctx,
		tutorID,
		createdClass.FirstSession.SessionID,
		"attendance-stale-second",
		SaveAttendanceInput{
			Revision: sheet.Revision,
			Marks:    []AttendanceMarkInput{{StudentID: student.StudentID, State: "Absent"}},
		},
	)
	var stale *ConflictError
	require.ErrorAs(t, err, &stale)
	require.Equal(t, "attendance_changed", stale.Code)
	require.IsType(t, AttendanceChangedDetails{}, stale.Details)

	var attendanceEvents int
	require.NoError(t, pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM outbox
		WHERE tutor_id = $1
		  AND event_name = $2`, tutorID, vermouth.EventAttendanceMarked).Scan(&attendanceEvents))
	require.Equal(t, 1, attendanceEvents)
}
