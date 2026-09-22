//nolint:testpackage // Golden vectors lock the unexported canonical struct order and explicit nulls.
package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

// covers: AC-4
func TestBillingService_DefaultPeriodUsesTheVerifiedTimezone(t *testing.T) {
	t.Parallel()

	service := &BillingService{now: func() time.Time {
		return time.Date(2026, time.September, 1, 0, 30, 0, 0, time.UTC)
	}}
	tests := []struct {
		name       string
		timezone   string
		serverDate string
		year       int
		month      int
	}{
		{
			name:       "Ho Chi Minh City has entered September",
			timezone:   "Asia/Ho_Chi_Minh",
			serverDate: "2026-09-01",
			year:       2026,
			month:      8,
		},
		{
			name:       "New York remains in August",
			timezone:   "America/New_York",
			serverDate: "2026-08-31",
			year:       2026,
			month:      7,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := service.DefaultPeriod(test.timezone)

			require.NoError(t, err)
			require.Equal(t, test.serverDate, result.ServerDate)
			require.Equal(t, test.timezone, result.Timezone)
			require.Equal(t, test.year, result.Year)
			require.Equal(t, test.month, result.Month)
			require.Equal(t, billingMinimumYear, result.MinimumYear)
		})
	}
}

// covers: AC-4
func TestBillingService_ValidatePeriodAcceptsOnlyCompletedMonths(t *testing.T) {
	t.Parallel()

	service := &BillingService{now: func() time.Time {
		return time.Date(2026, time.September, 22, 3, 0, 0, 0, time.UTC)
	}}
	start, end, err := service.validatePeriod(
		BillingPeriod{Year: 2026, Month: 8},
		"Asia/Ho_Chi_Minh",
	)
	require.NoError(t, err)
	require.Equal(t, "2026-08-01", start.Format(time.DateOnly))
	require.Equal(t, "2026-08-31", end.Format(time.DateOnly))

	tests := []struct {
		name     string
		period   BillingPeriod
		timezone string
		code     string
	}{
		{"current month", BillingPeriod{Year: 2026, Month: 9}, "Asia/Ho_Chi_Minh", "period_not_completed"},
		{"future month", BillingPeriod{Year: 2026, Month: 10}, "Asia/Ho_Chi_Minh", "period_not_completed"},
		{"year before minimum", BillingPeriod{Year: 1999, Month: 12}, "Asia/Ho_Chi_Minh", "invalid_period"},
		{"invalid month", BillingPeriod{Year: 2026, Month: 13}, "Asia/Ho_Chi_Minh", "invalid_period"},
		{"invalid timezone", BillingPeriod{Year: 2026, Month: 8}, "Mars/Olympus", "invalid_timezone"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, _, testErr := service.validatePeriod(test.period, test.timezone)

			var periodErr *BillingPeriodError
			require.ErrorAs(t, testErr, &periodErr)
			require.Equal(t, test.code, periodErr.Code)
			require.Equal(t, http.StatusBadRequest, periodErr.Status)
		})
	}
}

// covers: AC-7, AC-12
func TestBillingBlockersUseTypedRecoveryTargetsAndStableOrder(t *testing.T) {
	t.Parallel()

	studentID := uuid.MustParse("01996765-8050-7000-8000-000000000001")
	classID := uuid.MustParse("01996765-8050-7000-8000-000000000002")
	sessionID := uuid.MustParse("01996765-8050-7000-8000-000000000003")
	row := &sqlcgen.ListBillingCandidatesRow{
		StudentID: studentID,
		ClassID:   classID,
		SessionID: sessionID,
	}
	attendance := attendanceBlocker(row, "2026-08-18")
	rate := rateBlocker(row, "2026-08-18")

	require.Equal(t, "/", attendance.Destination.Route)
	require.Equal(t, "2026-08-18", *attendance.Destination.Date)
	require.Equal(t, sessionID, *attendance.Destination.SessionID)
	require.Equal(t, "/classes/"+classID.String(), rate.Destination.Route)
	require.Equal(t, "2026-08-18", *rate.Destination.RateDate)
	require.Equal(t, classID, *rate.Destination.ClassID)

	field := "bank_code"
	blockers := []BillingBlocker{
		rate,
		{Code: "profile_incomplete", Field: &field},
		attendance,
	}
	sortBillingBlockers(blockers)
	require.Equal(t, []string{
		"attendance_incomplete",
		"profile_incomplete",
		"rate_missing",
	}, []string{blockers[0].Code, blockers[1].Code, blockers[2].Code})
}

// covers: AC-8, AC-24
func TestDecodeReadinessRequiresValidCertifiedEvidence(t *testing.T) {
	t.Parallel()

	generation := uuid.MustParse("01996765-8050-7000-8000-000000000001")
	evidence, err := decodeReadiness(
		generation,
		"certified",
		"teaching.events",
		[]byte(`{"teaching.events":"topic-id"}`),
		[]byte(`{"teaching.events":[0,1]}`),
		[]byte(`{"teaching.events":{"0":12,"1":9}}`),
	)
	require.NoError(t, err)
	require.Equal(t, generation, evidence.generation)
	require.Equal(t, "certified", evidence.state)
	require.Equal(t, "topic-id", evidence.topicIdentity["teaching.events"])
	require.Equal(t, []int32{0, 1}, evidence.partitionSet["teaching.events"])
	require.True(t, evidence.completed)

	_, err = decodeReadiness(
		generation,
		"certified",
		"teaching.events",
		[]byte(`{"teaching.events":`),
		[]byte(`{"teaching.events":[0]}`),
		[]byte(`{"teaching.events":{"0":12}}`),
	)
	require.ErrorContains(t, err, "decode readiness topic identity")

	_, err = decodeReadiness(
		generation,
		"certified",
		"teaching.events",
		[]byte(`{"teaching.events":"topic-id"}`),
		[]byte(`{"teaching.events":`),
		[]byte(`{"teaching.events":{"0":12}}`),
	)
	require.ErrorContains(t, err, "decode readiness partition set")
}

// covers: AC-6, AC-7, AC-12, AC-13, AC-24
func TestBillingFingerprint_GoldenVector(t *testing.T) {
	t.Parallel()
	tutorID := uuid.MustParse("01996765-8050-7000-8000-000000000001")
	studentID := uuid.MustParse("01996765-8050-7000-8000-000000000002")
	classID := uuid.MustParse("01996765-8050-7000-8000-000000000003")
	sessionID := uuid.MustParse("01996765-8050-7000-8000-000000000004")
	studentName := "Nguyễn Mai"
	className := "Toán 9A"
	legalName := "Nguyễn An"
	digest, err := billingFingerprint(fingerprintPayload{
		SchemaVersion: billingFingerprintV1, ReadinessVersion: billingReadinessV1,
		TutorID: tutorID, PeriodYear: 2026, PeriodMonth: 8,
		Profile: fingerprintProfile{
			Revision: 4, LegalName: &legalName,
			ContactLine: nil, BankCode: nil, BankName: nil,
			AccountNumber: nil, AccountHolder: nil,
		},
		Candidates: []fingerprintCandidate{
			{
				StudentID: studentID, StudentName: &studentName,
				SessionID: sessionID, ClassID: classID, ClassName: &className,
				LocalDate: "2026-08-15", Attendance: "Present",
				Rate: &fingerprintRate{
					Amount: 0, Currency: billingCurrencyVND,
					EffectiveFrom: "2026-08-01", Revision: 2,
				},
			},
		},
		Blockers:      []BillingBlocker{},
		StudentTotals: []fingerprintStudentTotal{{StudentID: studentID, Total: 0}},
		GrandTotal:    0,
	})
	require.NoError(t, err)
	require.Equal(t, "Q3NacHsJIss8YpO4hdrSVfQjrL1zYXS6aTnLVr1ufr8", digest)
}
