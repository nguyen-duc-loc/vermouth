package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

const (
	billingTeachingConsumer = "billing.teaching"
	billingMinimumYear      = 2000
	billingMaxCandidates    = 10_000
	billingCandidateLimit   = billingMaxCandidates + 1
	billingMaxStudents      = 500
	billingBarrierTimeout   = 5 * time.Second
	billingBarrierPoll      = 50 * time.Millisecond
	billingRetryAfter       = 1
	billingFingerprintV1    = 1
	billingReadinessV1      = 1
	billingIssueRetries     = 3
	billingCurrencyVND      = "VND"
	codePeriodTooLarge      = "period_too_large"
)

var errIssuedRunUnreadable = errors.New("issued billing run was not readable")

// BillingPeriod identifies one closed calendar month in the tutor timezone.
type BillingPeriod struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}

// BillingPeriodDefault is the server derived completed month used by the UI.
type BillingPeriodDefault struct {
	ServerDate  string `json:"server_date"`
	Timezone    string `json:"timezone"`
	Year        int    `json:"year"`
	Month       int    `json:"month"`
	MinimumYear int    `json:"minimum_year"`
}

// BillingRecoveryDestination is a typed browser recovery target.
type BillingRecoveryDestination struct {
	Route     string     `json:"route"`
	Date      *string    `json:"date"`
	SessionID *uuid.UUID `json:"session_id"`
	ClassID   *uuid.UUID `json:"class_id"`
	RateDate  *string    `json:"rate_date"`
}

// BillingBlocker is one stable reason issue cannot proceed.
type BillingBlocker struct {
	Code        string                     `json:"code"`
	StudentID   *uuid.UUID                 `json:"student_id"`
	SessionID   *uuid.UUID                 `json:"session_id"`
	ClassID     *uuid.UUID                 `json:"class_id"`
	LocalDate   *string                    `json:"local_date"`
	Field       *string                    `json:"field"`
	Destination BillingRecoveryDestination `json:"destination"`
}

// BillingLine is one prospective or frozen Present session charge.
type BillingLine struct {
	SessionID  uuid.UUID  `json:"session_id"`
	ClassID    *uuid.UUID `json:"class_id"`
	ClassName  string     `json:"class_name"`
	LocalDate  string     `json:"local_date"`
	RateAmount int64      `json:"rate_amount"`
	Amount     int64      `json:"amount"`
	Currency   string     `json:"currency"`
}

// BillingStudentTotal groups all classes into one prospective invoice.
type BillingStudentTotal struct {
	StudentID   uuid.UUID     `json:"student_id"`
	StudentName string        `json:"student_name"`
	Lines       []BillingLine `json:"lines"`
	TotalAmount int64         `json:"total_amount"`
	Currency    string        `json:"currency"`
}

// BillingPreview is a temporary reviewed calculation and never a database row.
type BillingPreview struct {
	Status      string                `json:"status"`
	Period      BillingPeriod         `json:"period"`
	Students    []BillingStudentTotal `json:"students"`
	Blockers    []BillingBlocker      `json:"blockers"`
	GrandTotal  int64                 `json:"grand_total"`
	Currency    string                `json:"currency"`
	Fingerprint *string               `json:"preview_fingerprint"`
	Run         *BillingRunResult     `json:"run"`
}

// IssuedInvoice is one immutable invoice with its frozen lines.
type IssuedInvoice struct {
	InvoiceID     uuid.UUID     `json:"invoice_id"`
	StudentID     uuid.UUID     `json:"student_id"`
	StudentName   string        `json:"student_name"`
	InvoiceNumber string        `json:"invoice_number"`
	TotalAmount   int64         `json:"total_amount"`
	Currency      string        `json:"currency"`
	IssuedAt      time.Time     `json:"issued_at"`
	Lines         []BillingLine `json:"lines"`
}

// BillingRunResult is the stable response for a new or repeated issue.
type BillingRunResult struct {
	BillingRunID uuid.UUID       `json:"billing_run_id"`
	Period       BillingPeriod   `json:"period"`
	Generation   int             `json:"generation"`
	CreatedAt    time.Time       `json:"created_at"`
	Invoices     []IssuedInvoice `json:"invoices"`
	GrandTotal   int64           `json:"grand_total"`
	Currency     string          `json:"currency"`
}

// BillingPeriodState reports whether a completed month is issued.
type BillingPeriodState struct {
	Status string            `json:"status"`
	Period BillingPeriod     `json:"period"`
	Run    *BillingRunResult `json:"run"`
}

// IssueBillingInput carries the opaque digest from an explicit preview.
type IssueBillingInput struct {
	PreviewFingerprint string `json:"preview_fingerprint"`
}

// BillingPeriodError is a stable boundary outcome with safe details.
type BillingPeriodError struct {
	Code       string
	Message    string
	Status     int
	RetryAfter int
	Details    any
}

func (e *BillingPeriodError) Error() string { return e.Message }

// BillingService owns period reads, reviewed calculation, and atomic issue.
type BillingService struct {
	pool          *pgxpool.Pool
	admin         *kadm.Client
	logger        *slog.Logger
	publishTopic  string
	consumerName  string
	teachingTopic string
	now           func() time.Time
}

// NewBillingService binds billing work to its database and existing broker client.
func NewBillingService(
	pool *pgxpool.Pool,
	broker *kgo.Client,
	logger *slog.Logger,
	publishTopic string,
) *BillingService {
	return &BillingService{
		pool: pool, admin: kadm.NewClient(broker), logger: logger,
		publishTopic: publishTopic, now: time.Now,
		consumerName: billingTeachingConsumer, teachingTopic: vermouth.TopicTeaching,
	}
}

// DefaultPeriod derives the most recent completed month from one server clock.
func (s *BillingService) DefaultPeriod(timezone string) (BillingPeriodDefault, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return BillingPeriodDefault{}, &BillingPeriodError{
			Code: "invalid_timezone", Message: "the verified timezone is invalid", Status: http.StatusBadRequest,
		}
	}
	serverDate := s.now().UTC().In(location)
	completed := time.Date(serverDate.Year(), serverDate.Month(), 1, 0, 0, 0, 0, location).AddDate(0, -1, 0)
	return BillingPeriodDefault{
		ServerDate: serverDate.Format(time.DateOnly), Timezone: timezone,
		Year: completed.Year(), Month: int(completed.Month()), MinimumYear: billingMinimumYear,
	}, nil
}

func (s *BillingService) validatePeriod(
	period BillingPeriod,
	timezone string,
) (periodStart time.Time, periodEnd time.Time, err error) {
	if period.Year < billingMinimumYear || period.Year > math.MaxInt32 ||
		period.Month < 1 || period.Month > 12 {
		return time.Time{}, time.Time{}, &BillingPeriodError{
			Code: "invalid_period", Message: "year and month must name a completed month from year 2000", Status: http.StatusBadRequest,
		}
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, time.Time{}, &BillingPeriodError{
			Code: "invalid_timezone", Message: "the verified timezone is invalid", Status: http.StatusBadRequest,
		}
	}
	start := time.Date(period.Year, time.Month(period.Month), 1, 0, 0, 0, 0, location)
	now := s.now().In(location)
	currentStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location)
	if !start.Before(currentStart) {
		return time.Time{}, time.Time{}, &BillingPeriodError{
			Code: "period_not_completed", Message: "billing is available only for completed months", Status: http.StatusBadRequest,
		}
	}
	return start, start.AddDate(0, 1, -1), nil
}

func databasePeriod(period BillingPeriod) (year int32, month int32) {
	// validatePeriod proves both values fit before any database path calls this helper.
	return int32(period.Year), int32(period.Month) //nolint:gosec // The validated bounds fit both conversions.
}

type barrierCut struct {
	generation uuid.UUID
	ends       map[int32]int64
}

type readinessEvidence struct {
	generation    uuid.UUID
	state         string
	sourceTopics  []string
	topicIdentity map[string]string
	partitionSet  map[string][]int32
	completed     bool
}

func decodeReadiness(
	generation uuid.UUID,
	state string,
	sourceTopic string,
	topicIdentityJSON []byte,
	partitionSetJSON []byte,
	completedOffsets []byte,
) (readinessEvidence, error) {
	evidence := readinessEvidence{
		generation: generation, state: state,
		sourceTopics: strings.Split(sourceTopic, ","),
		completed:    len(completedOffsets) > 0,
	}
	err := json.Unmarshal(topicIdentityJSON, &evidence.topicIdentity)
	if err != nil {
		return readinessEvidence{}, fmt.Errorf("decode readiness topic identity: %w", err)
	}
	err = json.Unmarshal(partitionSetJSON, &evidence.partitionSet)
	if err != nil {
		return readinessEvidence{}, fmt.Errorf("decode readiness partition set: %w", err)
	}
	return evidence, nil
}

func projectionError(code, message string, status int) *BillingPeriodError {
	return &BillingPeriodError{Code: code, Message: message, Status: status}
}

//nolint:funlen // One five second context must cover metadata capture and committed offset polling.
func (s *BillingService) captureBarrier(ctx context.Context) (barrierCut, error) {
	barrierCtx, cancel := context.WithTimeout(ctx, billingBarrierTimeout)
	defer cancel()
	row, err := vermouth.ReadConsumerReadiness(barrierCtx, s.pool, s.consumerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return barrierCut{}, projectionError(
			"projection_uncertified", "the billing projection needs a certified replay", http.StatusConflict,
		)
	}
	if err != nil {
		return barrierCut{}, projectionError(
			"projection_unavailable", "billing could not read projection readiness", http.StatusServiceUnavailable,
		)
	}
	evidence, err := decodeReadiness(
		row.ProjectionGeneration, row.State, row.SourceTopic,
		[]byte(row.TopicIdentity), row.PartitionSet, row.CompletedOffsets,
	)
	if err != nil || evidence.state != "certified" || !evidence.completed {
		return barrierCut{}, projectionError(
			"projection_uncertified", "the billing projection needs a certified replay", http.StatusConflict,
		)
	}
	metadataErr := s.validateTeachingMetadata(barrierCtx, evidence)
	if metadataErr != nil {
		return barrierCut{}, metadataErr
	}
	ends, err := s.admin.ListEndOffsets(barrierCtx, s.teachingTopic)
	if err != nil || ends.Error() != nil {
		return barrierCut{}, projectionError(
			"projection_unavailable", "billing could not read the teaching event barrier", http.StatusServiceUnavailable,
		)
	}
	cut := barrierCut{generation: evidence.generation, ends: make(map[int32]int64)}
	for partition, offset := range ends[s.teachingTopic] {
		cut.ends[partition] = offset.Offset
	}

	ticker := time.NewTicker(billingBarrierPoll)
	defer ticker.Stop()
	var lastFetchErr error
	for {
		committed, fetchErr := s.admin.FetchOffsets(barrierCtx, s.consumerName)
		lastFetchErr = fetchErr
		if fetchErr == nil && committed.Error() == nil && barrierReached(committed, s.teachingTopic, cut.ends) {
			return cut, nil
		}
		if fetchErr == nil {
			lastFetchErr = committed.Error()
		}
		select {
		case <-barrierCtx.Done():
			if lastFetchErr != nil {
				return barrierCut{}, projectionError(
					"projection_unavailable", "billing could not read projection progress", http.StatusServiceUnavailable,
				)
			}
			return barrierCut{}, &BillingPeriodError{
				Code: "projection_sync_pending", Message: "billing is still catching up with teaching",
				Status: http.StatusServiceUnavailable, RetryAfter: billingRetryAfter,
			}
		case <-ticker.C:
		}
	}
}

func barrierReached(committed kadm.OffsetResponses, topic string, ends map[int32]int64) bool {
	for partition, end := range ends {
		offset, found := committed.Lookup(topic, partition)
		if !found || offset.Err != nil || offset.At < end {
			return false
		}
	}
	return true
}

func (s *BillingService) validateTeachingMetadata(
	ctx context.Context,
	evidence readinessEvidence,
) error {
	details, err := s.admin.ListTopics(ctx, s.teachingTopic)
	if err != nil {
		return projectionError("projection_unavailable", "billing could not read teaching topic metadata", http.StatusServiceUnavailable)
	}
	detail, found := details[s.teachingTopic]
	if !found || detail.Err != nil {
		return projectionError("projection_unavailable", "billing could not read teaching topic metadata", http.StatusServiceUnavailable)
	}
	if evidence.topicIdentity[s.teachingTopic] != detail.ID.String() {
		return projectionError("projection_uncertified", "the teaching topic identity changed", http.StatusConflict)
	}
	want := evidence.partitionSet[s.teachingTopic]
	got := make([]int32, 0, len(detail.Partitions))
	for partition := range detail.Partitions {
		got = append(got, partition)
	}
	slices.Sort(got)
	if len(got) != len(want) {
		return projectionError("projection_uncertified", "the teaching partition set changed", http.StatusConflict)
	}
	for index := range got {
		if got[index] != want[index] {
			return projectionError("projection_uncertified", "the teaching partition set changed", http.StatusConflict)
		}
	}
	return nil
}

type calculation struct {
	preview     BillingPreview
	profile     store.InvoiceProfile
	fingerprint string
}

type fingerprintProfile struct {
	Revision      int64   `json:"revision"`
	LegalName     *string `json:"legal_name"`
	ContactLine   *string `json:"contact_line"`
	BankCode      *string `json:"bank_code"`
	BankName      *string `json:"bank_name"`
	AccountNumber *string `json:"account_number"`
	AccountHolder *string `json:"account_holder"`
}

type fingerprintRate struct {
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency"`
	EffectiveFrom string `json:"effective_from"`
	Revision      int64  `json:"revision"`
}

type fingerprintCandidate struct {
	StudentID   uuid.UUID        `json:"student_id"`
	StudentName *string          `json:"student_name"`
	SessionID   uuid.UUID        `json:"session_id"`
	ClassID     uuid.UUID        `json:"class_id"`
	ClassName   *string          `json:"class_name"`
	LocalDate   string           `json:"local_date"`
	Attendance  string           `json:"attendance"`
	Rate        *fingerprintRate `json:"rate"`
}

type fingerprintStudentTotal struct {
	StudentID uuid.UUID `json:"student_id"`
	Total     int64     `json:"total"`
}

type fingerprintPayload struct {
	SchemaVersion    int                       `json:"schema_version"`
	ReadinessVersion int                       `json:"readiness_version"`
	TutorID          uuid.UUID                 `json:"tutor_id"`
	PeriodYear       int                       `json:"period_year"`
	PeriodMonth      int                       `json:"period_month"`
	Profile          fingerprintProfile        `json:"profile"`
	Candidates       []fingerprintCandidate    `json:"candidates"`
	Blockers         []BillingBlocker          `json:"blockers"`
	StudentTotals    []fingerprintStudentTotal `json:"student_totals"`
	GrandTotal       int64                     `json:"grand_total"`
}

//nolint:gocognit,funlen,maintidx // The canonical calculation keeps blockers, totals, and fingerprint ordering together.
func (s *BillingService) calculate(
	ctx context.Context,
	tx pgx.Tx,
	tutorID uuid.UUID,
	period BillingPeriod,
	periodStart time.Time,
	periodEnd time.Time,
	cut barrierCut,
) (calculation, error) {
	queries := store.Queries(tx)
	readiness, err := vermouth.LockConsumerReadinessShare(ctx, tx, s.consumerName)
	if errors.Is(err, pgx.ErrNoRows) {
		return calculation{}, projectionError(
			"projection_uncertified", "the billing projection needs a certified replay", http.StatusConflict,
		)
	}
	if err != nil {
		return calculation{}, fmt.Errorf("lock teaching readiness: %w", err)
	}
	if readiness.State != "certified" || readiness.ProjectionGeneration != cut.generation ||
		len(readiness.CompletedOffsets) == 0 {
		return calculation{}, projectionError(
			"projection_uncertified", "the billing projection certification changed", http.StatusConflict,
		)
	}
	failures, err := vermouth.ListUnresolvedConsumerFailures(
		ctx, tx, s.consumerName, tutorID,
	)
	if err != nil {
		return calculation{}, fmt.Errorf("read unresolved projection failures: %w", err)
	}
	if len(failures) > 0 {
		details := make([]map[string]any, 0, len(failures))
		for index := range failures {
			failure := &failures[index]
			details = append(details, map[string]any{
				"failure_category": failure.FailureCategory,
				"source_topic":     failure.SourceTopic,
				"source_partition": failure.SourcePartition,
				"source_offset":    failure.SourceOffset,
			})
		}
		return calculation{}, &BillingPeriodError{
			Code: "projection_failed", Message: "the teaching projection has an unresolved failure",
			Status: http.StatusConflict, Details: map[string]any{"failures": details},
		}
	}

	rows, err := queries.ListBillingCandidates(ctx, sqlcgen.ListBillingCandidatesParams{
		RowLimit: billingCandidateLimit, OwnerTutorID: tutorID,
		PeriodStart: pgtype.Date{Time: periodStart, Valid: true},
		PeriodEnd:   pgtype.Date{Time: periodEnd, Valid: true},
	})
	if err != nil {
		return calculation{}, fmt.Errorf("read billing candidates: %w", err)
	}
	if len(rows) > billingMaxCandidates {
		return calculation{}, &BillingPeriodError{
			Code: codePeriodTooLarge, Message: "the month exceeds the 10000 candidate limit", Status: http.StatusUnprocessableEntity,
		}
	}
	distinctStudents := make(map[uuid.UUID]struct{})
	for index := range rows {
		distinctStudents[rows[index].StudentID] = struct{}{}
	}
	if len(distinctStudents) > billingMaxStudents {
		return calculation{}, &BillingPeriodError{
			Code: codePeriodTooLarge, Message: "the month exceeds the 500 student limit", Status: http.StatusUnprocessableEntity,
		}
	}

	profile, profileErr := queries.GetInvoiceProfile(ctx, tutorID)
	if profileErr != nil && !errors.Is(profileErr, pgx.ErrNoRows) {
		return calculation{}, fmt.Errorf("read invoice profile for calculation: %w", profileErr)
	}
	if errors.Is(profileErr, pgx.ErrNoRows) {
		profile = store.InvoiceProfile{TutorID: tutorID}
	}
	preview := BillingPreview{
		Status: "empty", Period: period,
		Students: []BillingStudentTotal{}, Blockers: []BillingBlocker{},
		Currency: billingCurrencyVND,
	}
	fingerprintCandidates := make([]fingerprintCandidate, 0, len(rows))
	studentIndexes := make(map[uuid.UUID]int)
	for rowIndex := range rows {
		row := &rows[rowIndex]
		if row.CoverageCount != 1 || !row.ClassName.Valid || !row.StudentName.Valid {
			return calculation{}, &BillingPeriodError{
				Code: "projection_incomplete", Message: "the teaching projection is structurally incomplete",
				Status: http.StatusConflict,
				Details: map[string]any{
					"session_id": row.SessionID,
					"class_id":   row.ClassID,
					"student_id": row.StudentID,
				},
			}
		}
		attendance := "Unmarked"
		if row.AttendanceState.Valid {
			attendance = row.AttendanceState.String
		}
		var rate *fingerprintRate
		if row.RateKnown {
			rate = &fingerprintRate{
				Amount: row.RateAmount, Currency: row.Currency,
				EffectiveFrom: row.RateEffectiveFrom.Time.Format(time.DateOnly),
				Revision:      row.RateRevision,
			}
		}
		localDate := row.LocalDate.Time.Format(time.DateOnly)
		studentName := row.StudentName.String
		className := row.ClassName.String
		fingerprintCandidates = append(fingerprintCandidates, fingerprintCandidate{
			StudentID: row.StudentID, StudentName: &studentName,
			SessionID: row.SessionID, ClassID: row.ClassID, ClassName: &className,
			LocalDate: localDate, Attendance: attendance, Rate: rate,
		})
		switch attendance {
		case "Unmarked":
			preview.Blockers = append(preview.Blockers, attendanceBlocker(row, localDate))
		case "Present":
			if !row.RateKnown {
				preview.Blockers = append(preview.Blockers, rateBlocker(row, localDate))
				continue
			}
			studentIndex, found := studentIndexes[row.StudentID]
			if !found {
				studentIndex = len(preview.Students)
				studentIndexes[row.StudentID] = studentIndex
				preview.Students = append(preview.Students, BillingStudentTotal{
					StudentID: row.StudentID, StudentName: studentName,
					Lines: []BillingLine{}, Currency: billingCurrencyVND,
				})
			}
			student := &preview.Students[studentIndex]
			classID := row.ClassID
			if row.RateAmount > math.MaxInt64-student.TotalAmount ||
				row.RateAmount > math.MaxInt64-preview.GrandTotal {
				return calculation{}, &BillingPeriodError{
					Code: codePeriodTooLarge, Message: "the month total exceeds the supported money range", Status: http.StatusUnprocessableEntity,
				}
			}
			student.Lines = append(student.Lines, BillingLine{
				SessionID: row.SessionID, ClassID: &classID, ClassName: className,
				LocalDate: localDate, RateAmount: row.RateAmount, Amount: row.RateAmount,
				Currency: billingCurrencyVND,
			})
			student.TotalAmount += row.RateAmount
			preview.GrandTotal += row.RateAmount
		case "Absent":
		default:
			return calculation{}, &BillingPeriodError{
				Code: "projection_incomplete", Message: "the projection contains an unknown attendance state", Status: http.StatusConflict,
				Details: map[string]any{"session_id": row.SessionID, "student_id": row.StudentID},
			}
		}
	}

	if len(preview.Students) > 0 {
		for _, field := range MissingProfileFields(profile) {
			preview.Blockers = append(preview.Blockers, BillingBlocker{
				Code: "profile_incomplete", Field: &field,
				Destination: BillingRecoveryDestination{Route: "/profile"},
			})
		}
	}
	sortBillingBlockers(preview.Blockers)
	studentTotals := make([]fingerprintStudentTotal, 0, len(preview.Students))
	for _, student := range preview.Students {
		studentTotals = append(studentTotals, fingerprintStudentTotal{
			StudentID: student.StudentID, Total: student.TotalAmount,
		})
	}
	payload := fingerprintPayload{
		SchemaVersion: billingFingerprintV1, ReadinessVersion: billingReadinessV1,
		TutorID: tutorID, PeriodYear: period.Year, PeriodMonth: period.Month,
		Profile: fingerprintProfile{
			Revision:  profile.Revision,
			LegalName: textPointer(profile.LegalName), ContactLine: textPointer(profile.ContactLine),
			BankCode: textPointer(profile.BankCode), BankName: textPointer(profile.BankName),
			AccountNumber: textPointer(profile.BankAccountNumber), AccountHolder: textPointer(profile.BankAccountHolder),
		},
		Candidates: fingerprintCandidates, Blockers: preview.Blockers,
		StudentTotals: studentTotals, GrandTotal: preview.GrandTotal,
	}
	fingerprint, err := billingFingerprint(payload)
	if err != nil {
		return calculation{}, err
	}
	if len(preview.Blockers) > 0 {
		preview.Status = "blocked"
	} else if len(preview.Students) > 0 {
		preview.Status = "ready"
		preview.Fingerprint = &fingerprint
	}
	return calculation{preview: preview, profile: profile, fingerprint: fingerprint}, nil
}

func billingFingerprint(payload fingerprintPayload) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode billing fingerprint: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func attendanceBlocker(row *sqlcgen.ListBillingCandidatesRow, localDate string) BillingBlocker {
	date := localDate
	sessionID := row.SessionID
	studentID := row.StudentID
	classID := row.ClassID
	return BillingBlocker{
		Code: "attendance_incomplete", StudentID: &studentID, SessionID: &sessionID,
		ClassID: &classID, LocalDate: &date,
		Destination: BillingRecoveryDestination{
			Route: "/", Date: &date, SessionID: &sessionID,
		},
	}
}

func rateBlocker(row *sqlcgen.ListBillingCandidatesRow, localDate string) BillingBlocker {
	date := localDate
	sessionID := row.SessionID
	studentID := row.StudentID
	classID := row.ClassID
	return BillingBlocker{
		Code: "rate_missing", StudentID: &studentID, SessionID: &sessionID,
		ClassID: &classID, LocalDate: &date,
		Destination: BillingRecoveryDestination{
			Route: "/classes/" + classID.String(), ClassID: &classID, RateDate: &date,
		},
	}
}

func sortBillingBlockers(blockers []BillingBlocker) {
	slices.SortFunc(blockers, func(left, right BillingBlocker) int {
		return strings.Compare(blockerSortKey(left), blockerSortKey(right))
	})
}

func blockerSortKey(blocker BillingBlocker) string {
	return blocker.Code + "\x00" + uuidPointerText(blocker.StudentID) + "\x00" +
		stringPointerText(blocker.LocalDate) + "\x00" + uuidPointerText(blocker.SessionID) + "\x00" +
		stringPointerText(blocker.Field)
}

func uuidPointerText(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func stringPointerText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ReadPeriod returns authoritative issued data without broker access.
func (s *BillingService) ReadPeriod(
	ctx context.Context,
	tutorID uuid.UUID,
	timezone string,
	period BillingPeriod,
) (BillingPeriodState, error) {
	_, _, err := s.validatePeriod(period, timezone)
	if err != nil {
		return BillingPeriodState{}, err
	}
	run, found, err := s.readLiveRun(ctx, tutorID, period)
	if err != nil {
		return BillingPeriodState{}, err
	}
	if found {
		return BillingPeriodState{Status: "already_issued", Period: period, Run: &run}, nil
	}
	return BillingPeriodState{Status: "unissued", Period: period}, nil
}

// Preview waits for a certified published cut, then calculates in one repeatable read snapshot.
func (s *BillingService) Preview(
	ctx context.Context,
	tutorID uuid.UUID,
	timezone string,
	period BillingPeriod,
) (BillingPreview, error) {
	periodStart, periodEnd, err := s.validatePeriod(period, timezone)
	if err != nil {
		return BillingPreview{}, err
	}
	run, found, readErr := s.readLiveRun(ctx, tutorID, period)
	if readErr != nil {
		return BillingPreview{}, readErr
	}
	if found {
		return BillingPreview{
			Status: "already_issued", Period: period,
			Students: []BillingStudentTotal{}, Blockers: []BillingBlocker{},
			Currency: billingCurrencyVND, Run: &run,
		}, nil
	}
	cut, err := s.captureBarrier(ctx)
	if err != nil {
		return BillingPreview{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead,
	})
	if err != nil {
		return BillingPreview{}, fmt.Errorf("begin billing preview: %w", err)
	}
	defer s.rollback(ctx, tx, "billing preview")
	result, err := s.calculate(ctx, tx, tutorID, period, periodStart, periodEnd, cut)
	if err != nil {
		return BillingPreview{}, err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return BillingPreview{}, fmt.Errorf("commit billing preview snapshot: %w", err)
	}
	s.logger.InfoContext(ctx, "Billing preview calculated",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("tutor_id", tutorID.String()),
		slog.Int("period_year", period.Year),
		slog.Int("period_month", period.Month),
		slog.Int("candidate_students", len(result.preview.Students)),
		slog.Int("blocker_count", len(result.preview.Blockers)),
		slog.String("outcome", result.preview.Status),
	)
	return result.preview, nil
}

// Issue recomputes the preview proof and writes the whole run in one transaction.
//
//nolint:gocognit // Retry, unique winner recovery, and serialization handling are one issue protocol.
func (s *BillingService) Issue(
	ctx context.Context,
	tutorID uuid.UUID,
	timezone string,
	period BillingPeriod,
	input IssueBillingInput,
) (BillingRunResult, int, error) {
	periodStart, periodEnd, err := s.validatePeriod(period, timezone)
	if err != nil {
		return BillingRunResult{}, 0, err
	}
	existingRun, found, readErr := s.readLiveRun(ctx, tutorID, period)
	if readErr != nil {
		return BillingRunResult{}, 0, readErr
	}
	if found {
		return existingRun, http.StatusOK, nil
	}
	if input.PreviewFingerprint == "" {
		return BillingRunResult{}, 0, previewStale()
	}
	cut, err := s.captureBarrier(ctx)
	if err != nil {
		return BillingRunResult{}, 0, err
	}
	for attempt := 1; attempt <= billingIssueRetries; attempt++ {
		err = s.issueOnce(
			ctx, tutorID, period, periodStart, periodEnd, cut, input.PreviewFingerprint,
		)
		if err == nil {
			issuedRun, issuedFound, issuedReadErr := s.readLiveRun(ctx, tutorID, period)
			if issuedReadErr != nil {
				return BillingRunResult{}, 0, issuedReadErr
			}
			if !issuedFound {
				return BillingRunResult{}, 0, errIssuedRunUnreadable
			}
			return issuedRun, http.StatusCreated, nil
		}
		pgErr, isPostgresError := errors.AsType[*pgconn.PgError](err)
		if !isPostgresError {
			return BillingRunResult{}, 0, err
		}
		if pgErr.Code == "23505" {
			concurrentRun, concurrentFound, concurrentReadErr := s.readLiveRun(ctx, tutorID, period)
			if concurrentReadErr != nil {
				return BillingRunResult{}, 0, concurrentReadErr
			}
			if concurrentFound {
				return concurrentRun, http.StatusOK, nil
			}
			return BillingRunResult{}, 0, fmt.Errorf("read concurrent billing winner: %w", err)
		}
		if pgErr.Code != "40001" || attempt == billingIssueRetries {
			return BillingRunResult{}, 0, err
		}
	}
	return BillingRunResult{}, 0, err
}

func previewStale() *BillingPeriodError {
	return &BillingPeriodError{
		Code: "preview_stale", Message: "billing facts changed after the reviewed preview", Status: http.StatusConflict,
	}
}

//nolint:gocognit,funlen // One transaction visibly owns every run, invoice, line, number, and outbox write.
func (s *BillingService) issueOnce(
	ctx context.Context,
	tutorID uuid.UUID,
	period BillingPeriod,
	periodStart time.Time,
	periodEnd time.Time,
	cut barrierCut,
	previewFingerprint string,
) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return fmt.Errorf("begin billing issue: %w", err)
	}
	defer s.rollback(ctx, tx, "billing issue")
	queries := store.Queries(tx)
	periodYear, periodMonth := databasePeriod(period)
	_, existingErr := queries.GetLiveBillingRunForPeriod(
		ctx,
		sqlcgen.GetLiveBillingRunForPeriodParams{
			TutorID: tutorID, PeriodYear: periodYear, PeriodMonth: periodMonth,
		},
	)
	if existingErr == nil {
		return &pgconn.PgError{Code: "23505", Message: "live billing run already exists"}
	} else if !errors.Is(existingErr, pgx.ErrNoRows) {
		return fmt.Errorf("read live billing run in issue: %w", existingErr)
	}
	result, err := s.calculate(ctx, tx, tutorID, period, periodStart, periodEnd, cut)
	if err != nil {
		periodErr, isPeriodError := errors.AsType[*BillingPeriodError](err)
		if isPeriodError && periodErr.Code == codePeriodTooLarge {
			return previewStale()
		}
		return err
	}
	if result.preview.Status != "ready" || result.fingerprint != previewFingerprint {
		return previewStale()
	}

	runID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate billing run id: %w", err)
	}
	run, err := queries.InsertBillingRun(ctx, sqlcgen.InsertBillingRunParams{
		BillingRunID: runID, TutorID: tutorID,
		PeriodYear: periodYear, PeriodMonth: periodMonth, Generation: 1,
	})
	if err != nil {
		return fmt.Errorf("insert billing run: %w", err)
	}
	issuedAt, err := queries.GetBillingTransactionTimestamp(
		ctx,
		sqlcgen.GetBillingTransactionTimestampParams{TutorID: tutorID, BillingRunID: run.BillingRunID},
	)
	if err != nil {
		return fmt.Errorf("read billing transaction time: %w", err)
	}
	for _, student := range result.preview.Students {
		sequence, takeErr := queries.TakeNextInvoiceNumber(
			ctx,
			sqlcgen.TakeNextInvoiceNumberParams{TutorID: tutorID, PeriodYear: periodYear},
		)
		if takeErr != nil {
			return fmt.Errorf("take invoice number: %w", takeErr)
		}
		invoiceID, idErr := uuid.NewV7()
		if idErr != nil {
			return fmt.Errorf("generate invoice id: %w", idErr)
		}
		invoiceNumber := fmt.Sprintf("%04d-%04d", period.Year, sequence)
		invoice, insertErr := queries.InsertInvoice(ctx, sqlcgen.InsertInvoiceParams{
			InvoiceID: invoiceID, TutorID: tutorID, BillingRunID: run.BillingRunID,
			StudentID: student.StudentID, InvoiceNumber: invoiceNumber,
			PeriodYear: periodYear, PeriodMonth: periodMonth,
			TotalAmount: student.TotalAmount, Currency: billingCurrencyVND, IssuedAt: issuedAt,
			StudentName:            student.StudentName,
			PayeeLegalName:         result.profile.LegalName.String,
			PayeeContactLine:       result.profile.ContactLine.String,
			PayeeBankName:          result.profile.BankName.String,
			PayeeBankAccountNumber: result.profile.BankAccountNumber.String,
			PayeeBankAccountHolder: result.profile.BankAccountHolder.String,
		})
		if insertErr != nil {
			return fmt.Errorf("insert invoice: %w", insertErr)
		}
		for _, line := range student.Lines {
			lineID, lineIDErr := uuid.NewV7()
			if lineIDErr != nil {
				return fmt.Errorf("generate invoice line id: %w", lineIDErr)
			}
			lineDate, parseErr := time.Parse(time.DateOnly, line.LocalDate)
			if parseErr != nil {
				return fmt.Errorf("parse invoice line date: %w", parseErr)
			}
			_, insertLineErr := queries.InsertInvoiceLine(ctx, sqlcgen.InsertInvoiceLineParams{
				InvoiceLineID: lineID, InvoiceID: invoice.InvoiceID, TutorID: tutorID,
				SessionID:   line.SessionID,
				SessionDate: pgtype.Date{Time: lineDate, Valid: true},
				ClassName:   line.ClassName, RateAmount: line.RateAmount, Amount: line.Amount,
			})
			if insertLineErr != nil {
				return fmt.Errorf("insert invoice line: %w", insertLineErr)
			}
		}
		writeErr := s.writeInvoiceIssued(ctx, tx, invoice, issuedAt)
		if writeErr != nil {
			return writeErr
		}
	}
	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit billing issue: %w", err)
	}
	s.logger.InfoContext(ctx, "Billing period issued",
		slog.String("request_id", vermouth.RequestID(ctx)),
		slog.String("tutor_id", tutorID.String()),
		slog.Int("period_year", period.Year),
		slog.Int("period_month", period.Month),
		slog.String("billing_run_id", run.BillingRunID.String()),
		slog.Int("invoice_count", len(result.preview.Students)),
		slog.String("outcome", "issued"),
	)
	return nil
}

func (s *BillingService) rollback(ctx context.Context, tx pgx.Tx, operation string) {
	err := tx.Rollback(ctx)
	if err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		s.logger.ErrorContext(ctx, "Roll back billing transaction",
			slog.String("operation", operation),
			slog.String("error", err.Error()),
		)
	}
}

type invoiceIssuedFields struct {
	InvoiceID     uuid.UUID `json:"invoice_id"`
	InvoiceNumber string    `json:"invoice_number"`
	TutorID       uuid.UUID `json:"tutor_id"`
	StudentID     uuid.UUID `json:"student_id"`
	PeriodYear    int32     `json:"period_year"`
	PeriodMonth   int32     `json:"period_month"`
	TotalAmount   int64     `json:"total_amount"`
	Currency      string    `json:"currency"`
	IssuedAt      time.Time `json:"issued_at"`
	PDFLocation   *string   `json:"pdf_location"`
}

func (s *BillingService) writeInvoiceIssued(
	ctx context.Context,
	tx pgx.Tx,
	invoice store.Invoice,
	issuedAt time.Time,
) error {
	envelope, err := vermouth.NewEnvelope(
		ctx,
		vermouth.EventInvoiceIssued,
		1,
		invoice.TutorID,
		vermouth.Key{Kind: vermouth.KeyInvoiceID, Value: invoice.InvoiceID},
		invoiceIssuedFields{
			InvoiceID: invoice.InvoiceID, InvoiceNumber: invoice.InvoiceNumber,
			TutorID: invoice.TutorID, StudentID: invoice.StudentID,
			PeriodYear: invoice.PeriodYear, PeriodMonth: invoice.PeriodMonth,
			TotalAmount: invoice.TotalAmount, Currency: invoice.Currency,
			IssuedAt: issuedAt, PDFLocation: nil,
		},
	)
	if err != nil {
		return fmt.Errorf("build invoice issued event: %w", err)
	}
	envelope.OccurredAt = issuedAt.UTC()
	return vermouth.WriteOutbox(ctx, tx, s.publishTopic, envelope)
}

func (s *BillingService) readLiveRun(
	ctx context.Context,
	tutorID uuid.UUID,
	period BillingPeriod,
) (BillingRunResult, bool, error) {
	queries := store.Queries(s.pool)
	periodYear, periodMonth := databasePeriod(period)
	run, err := queries.GetLiveBillingRunForPeriod(
		ctx,
		sqlcgen.GetLiveBillingRunForPeriodParams{
			TutorID: tutorID, PeriodYear: periodYear, PeriodMonth: periodMonth,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return BillingRunResult{}, false, nil
	}
	if err != nil {
		return BillingRunResult{}, false, fmt.Errorf("read live billing run: %w", err)
	}
	invoices, err := queries.ListInvoicesForRun(ctx, sqlcgen.ListInvoicesForRunParams{
		TutorID: tutorID, BillingRunID: run.BillingRunID,
	})
	if err != nil {
		return BillingRunResult{}, false, fmt.Errorf("read invoices for run: %w", err)
	}
	lines, err := queries.ListInvoiceLinesForRun(ctx, sqlcgen.ListInvoiceLinesForRunParams{
		TutorID: tutorID, BillingRunID: run.BillingRunID,
	})
	if err != nil {
		return BillingRunResult{}, false, fmt.Errorf("read invoice lines for run: %w", err)
	}
	lineGroups := make(map[uuid.UUID][]BillingLine, len(invoices))
	for index := range lines {
		line := &lines[index]
		lineGroups[line.InvoiceID] = append(lineGroups[line.InvoiceID], BillingLine{
			SessionID: line.SessionID, ClassName: line.ClassName,
			LocalDate:  line.SessionDate.Time.Format(time.DateOnly),
			RateAmount: line.RateAmount, Amount: line.Amount, Currency: billingCurrencyVND,
		})
	}
	result := BillingRunResult{
		BillingRunID: run.BillingRunID, Period: period, Generation: int(run.Generation),
		CreatedAt: run.CreatedAt, Invoices: []IssuedInvoice{}, Currency: billingCurrencyVND,
	}
	for index := range invoices {
		invoice := &invoices[index]
		result.Invoices = append(result.Invoices, IssuedInvoice{
			InvoiceID: invoice.InvoiceID, StudentID: invoice.StudentID,
			StudentName: invoice.StudentName, InvoiceNumber: invoice.InvoiceNumber,
			TotalAmount: invoice.TotalAmount, Currency: invoice.Currency,
			IssuedAt: invoice.IssuedAt, Lines: lineGroups[invoice.InvoiceID],
		})
		result.GrandTotal += invoice.TotalAmount
	}
	return result, true, nil
}

// ProjectedClassRate is one dated teaching fact held by billing.
type ProjectedClassRate struct {
	EffectiveFrom string `json:"effective_from"`
	RateAmount    int64  `json:"rate_amount"`
	Currency      string `json:"currency"`
	RateRevision  int64  `json:"rate_revision"`
}

// ClassRateHistory is the complete projected schedule for one class.
type ClassRateHistory struct {
	Rates             []ProjectedClassRate `json:"rates"`
	ProjectedRevision int64                `json:"projected_revision"`
}

// ReadClassRateHistory returns billing's projected dated rows for an owned tutor.
func (s *BillingService) ReadClassRateHistory(
	ctx context.Context,
	tutorID uuid.UUID,
	classID uuid.UUID,
) (ClassRateHistory, error) {
	rows, err := store.Queries(s.pool).ListOwnedClassRates(
		ctx,
		sqlcgen.ListOwnedClassRatesParams{TutorID: tutorID, ClassID: classID},
	)
	if err != nil {
		return ClassRateHistory{}, fmt.Errorf("read projected class rates: %w", err)
	}
	result := ClassRateHistory{Rates: make([]ProjectedClassRate, 0, len(rows))}
	for index := range rows {
		row := &rows[index]
		result.Rates = append(result.Rates, ProjectedClassRate{
			EffectiveFrom: row.EffectiveFrom.Time.Format(time.DateOnly),
			RateAmount:    row.RateAmount, Currency: row.Currency, RateRevision: row.RateRevision,
		})
		if row.RateRevision > result.ProjectedRevision {
			result.ProjectedRevision = row.RateRevision
		}
	}
	return result, nil
}
