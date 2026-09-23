//nolint:testpackage // These integration tests inspect the complete calendar projection from teaching truth.
package handler

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/stretchr/testify/require"
)

// covers: AC-10, AC-12, AC-13, AC-15
func TestReadSchedule_UsesDisplayZoneAndIncludesLatestPlusSourceRules(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	work := newTeachingHandler(t, pool, time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC))
	ctx := vermouth.WithRequestID(t.Context(), "request-schedule-zones")
	zoned, _, err := work.CreateClass(ctx, tutorID, "America/New_York", "zoned-calendar-class", CreateClassInput{
		Name: "Zoned class", Color: new("blue"), RateAmount: 150_000,
		Schedule: &WeeklyScheduleInput{
			ValidFrom: "2026-09-20", ValidThrough: "2026-09-20",
			Slots: []WeeklyScheduleSlotInput{{Weekday: 7, StartTime: "23:00", EndTime: "23:30"}},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, zoned.FirstSession)
	moved, _, err := work.MoveSession(
		ctx, tutorID, zoned.FirstSession.SessionID, "UTC", "zoned-calendar-move",
		MoveSessionInput{
			ExpectedVersion: 1, LocalDate: "2026-09-21", StartTime: "23:00", EndTime: "23:30",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "2026-09-21", moved.LocalDate)
	latest, _, err := work.PutSchedule(
		ctx, tutorID, zoned.Class.ClassID, "Asia/Ho_Chi_Minh", "zoned-calendar-latest",
		PutScheduleInput{
			ExpectedRevision: 1, EffectiveFrom: "2026-10-04", ValidThrough: "2026-10-04",
			Slots: []WeeklyScheduleSlotInput{{Weekday: 7, StartTime: "10:00", EndTime: "11:00"}},
		},
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), latest.Class.ScheduleRevision)
	alpha, _, err := work.CreateClass(ctx, tutorID, "UTC", "alpha-calendar-class", CreateClassInput{
		Name: "Alpha class", Color: new("violet"), RateAmount: 150_000,
		FirstSession: FirstSessionInput{
			LocalDate: "2026-09-21", StartTime: "08:00", EndTime: "09:00",
		},
	})
	require.NoError(t, err)

	newYork, err := work.ReadSchedule(
		ctx, tutorID, "America/New_York", "2026-09-21", "2026-09-21",
		[]uuid.UUID{zoned.Class.ClassID, uuid.Must(uuid.NewV7()), zoned.Class.ClassID}, false, 50, "",
	)
	require.NoError(t, err)
	require.Len(t, newYork.Sessions, 1)
	require.Equal(t, moved.SessionID, newYork.Sessions[0].SessionID)
	require.Equal(t, "2026-09-21", newYork.Sessions[0].DisplayDate)
	require.Equal(t, "23:00", newYork.Sessions[0].DisplayStart)
	require.Equal(t, "2026-09-21", newYork.Sessions[0].LocalDate)
	require.NotNil(t, newYork.Sessions[0].SourceTimeZone)
	require.Equal(t, "America/New_York", *newYork.Sessions[0].SourceTimeZone)
	require.Equal(t, "Zoned class", newYork.Sessions[0].ClassName)
	require.Equal(t, "blue", newYork.Sessions[0].ClassColor)
	require.Len(t, newYork.Rules, 2)
	require.Equal(t, []int64{1, 2}, []int64{newYork.Rules[0].Revision, newYork.Rules[1].Revision})
	onlyForeign, err := work.ReadSchedule(
		ctx, tutorID, "America/New_York", "2026-09-21", "2026-09-21",
		[]uuid.UUID{uuid.Must(uuid.NewV7())}, false, 50, "",
	)
	require.NoError(t, err)
	require.Empty(t, onlyForeign.Sessions)

	tokyo, err := work.ReadSchedule(
		ctx, tutorID, "Asia/Tokyo", "2026-09-22", "2026-09-22",
		[]uuid.UUID{zoned.Class.ClassID}, false, 50, "",
	)
	require.NoError(t, err)
	require.Len(t, tokyo.Sessions, 1)
	require.Equal(t, moved.SessionID, tokyo.Sessions[0].SessionID)
	require.Equal(t, "2026-09-22", tokyo.Sessions[0].DisplayDate)
	require.Equal(t, "12:00", tokyo.Sessions[0].DisplayStart)
	require.Equal(t, "+09:00", tokyo.Sessions[0].StartUTCOffset)
	require.Equal(t, "2026-09-21", tokyo.Sessions[0].LocalDate)

	allClasses, err := work.ReadSchedule(
		ctx, tutorID, "UTC", "2026-09-21", "2026-09-27", nil, false, 50, "",
	)
	require.NoError(t, err)
	require.Len(t, allClasses.Classes, 2)
	require.Equal(t, alpha.Class.ClassID, allClasses.Classes[0].ClassID)
	require.Equal(t, zoned.Class.ClassID, allClasses.Classes[1].ClassID)

	_, err = work.ReadSchedule(ctx, tutorID, "UTC", "2026-09-21", "2026-11-02", nil, false, 50, "")
	var tooWide *ValidationError
	require.ErrorAs(t, err, &tooWide)
	_, err = work.ReadSchedule(ctx, tutorID, "UTC", "2026-09-22", "2026-09-21", nil, false, 50, "")
	var reversed *ValidationError
	require.ErrorAs(t, err, &reversed)
}

// covers: AC-12, AC-13
func TestReadSchedule_PagesStableHistoryAndBindsCursor(t *testing.T) {
	t.Parallel()

	pool := teachingTestPool(t)
	tutorID := uuid.Must(uuid.NewV7())
	cleanTeachingTutor(t, pool, tutorID)
	work := newTeachingHandler(t, pool, time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC))
	ctx := vermouth.WithRequestID(t.Context(), "request-schedule-history")
	slots := []WeeklyScheduleSlotInput{
		{Weekday: 1, StartTime: "10:00", EndTime: "11:00"},
		{Weekday: 2, StartTime: "10:00", EndTime: "11:00"},
		{Weekday: 3, StartTime: "10:00", EndTime: "11:00"},
		{Weekday: 4, StartTime: "10:00", EndTime: "11:00"},
		{Weekday: 5, StartTime: "10:00", EndTime: "11:00"},
		{Weekday: 6, StartTime: "10:00", EndTime: "11:00"},
		{Weekday: 7, StartTime: "10:00", EndTime: "11:00"},
	}
	created := createScheduledClass(t, work, tutorID, "history-class", "2026-09-07", slots)
	replacementSlots := slices.Clone(slots)
	for index := range replacementSlots {
		replacementSlots[index].StartTime = "12:00"
		replacementSlots[index].EndTime = "13:00"
	}
	result, _, err := work.PutSchedule(
		ctx, tutorID, created.Class.ClassID, "UTC", "history-replacement",
		PutScheduleInput{
			ExpectedRevision: 1, EffectiveFrom: "2026-09-01",
			ValidThrough: "2026-09-07", Slots: replacementSlots,
		},
	)
	require.NoError(t, err)
	require.Equal(t, 7, result.SupersededCount)

	cursor := ""
	seen := make(map[uuid.UUID]struct{})
	ordered := make([]string, 0, 7)
	for {
		page, pageErr := work.ReadSchedule(
			ctx, tutorID, "UTC", "2026-09-01", "2026-09-07",
			[]uuid.UUID{created.Class.ClassID}, true, 2, cursor,
		)
		require.NoError(t, pageErr)
		for _, session := range page.ReplacedHistory {
			_, duplicate := seen[session.SessionID]
			require.False(t, duplicate)
			seen[session.SessionID] = struct{}{}
			ordered = append(ordered, session.OriginLocalDate+"/"+session.SessionID.String())
		}
		if page.NextHistoryCursor == nil {
			break
		}
		cursor = *page.NextHistoryCursor
	}
	require.Len(t, seen, 7)
	require.True(t, slices.IsSorted(ordered))

	firstPage, err := work.ReadSchedule(
		ctx, tutorID, "UTC", "2026-09-01", "2026-09-07",
		[]uuid.UUID{created.Class.ClassID}, true, 2, "",
	)
	require.NoError(t, err)
	require.NotNil(t, firstPage.NextHistoryCursor)
	validCursor := *firstPage.NextHistoryCursor
	decoded, err := decodeScheduleHistoryCursor(
		validCursor, tutorID, "2026-09-01", "2026-09-07",
		[]uuid.UUID{created.Class.ClassID}, 2,
	)
	require.NoError(t, err)
	require.NotNil(t, decoded)
	wrongOrder := *decoded
	wrongOrder.Order = 2
	wrongOrderCursor, err := encodeScheduleHistoryCursor(wrongOrder)
	require.NoError(t, err)

	tests := []struct {
		name     string
		tutorID  uuid.UUID
		from     string
		through  string
		classIDs []uuid.UUID
		limit    int
		cursor   string
	}{
		{
			name: "tutor", tutorID: uuid.Must(uuid.NewV7()), from: "2026-09-01", through: "2026-09-07",
			classIDs: []uuid.UUID{created.Class.ClassID}, limit: 2, cursor: validCursor,
		},
		{
			name: "window", tutorID: tutorID, from: "2026-09-02", through: "2026-09-07",
			classIDs: []uuid.UUID{created.Class.ClassID}, limit: 2, cursor: validCursor,
		},
		{
			name: "filters", tutorID: tutorID, from: "2026-09-01", through: "2026-09-07",
			classIDs: []uuid.UUID{uuid.Must(uuid.NewV7())}, limit: 2, cursor: validCursor,
		},
		{
			name: "limit", tutorID: tutorID, from: "2026-09-01", through: "2026-09-07",
			classIDs: []uuid.UUID{created.Class.ClassID}, limit: 3, cursor: validCursor,
		},
		{
			name: "order", tutorID: tutorID, from: "2026-09-01", through: "2026-09-07",
			classIDs: []uuid.UUID{created.Class.ClassID}, limit: 2, cursor: wrongOrderCursor,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, readErr := work.ReadSchedule(
				ctx, test.tutorID, "UTC", test.from, test.through,
				test.classIDs, true, test.limit, test.cursor,
			)
			var validation *ValidationError
			require.ErrorAs(t, readErr, &validation)
			require.Equal(t, historyCursorField, validation.Field)
		})
	}
}
