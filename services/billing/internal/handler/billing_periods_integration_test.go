//go:build integration

//nolint:paralleltest,tparallel,testpackage // Private broker names isolate consumers. Serial suites stay within the shared database connection budget.
package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

type billingProtocolFixture struct {
	service      *BillingService
	pool         *pgxpool.Pool
	broker       *kgo.Client
	tutorID      uuid.UUID
	classID      uuid.UUID
	period       BillingPeriod
	trace        *billingQueryTrace
	progressRead <-chan struct{}
}

type billingOffsetObserver struct{ fetched chan struct{} }

func (observer *billingOffsetObserver) OnBrokerRead(_ kgo.BrokerMetadata, key int16, _ int, _ time.Duration, _ time.Duration, _ error) {
	if key == 9 {
		select {
		case observer.fetched <- struct{}{}:
		default:
		}
	}
}

type billingQueryTrace struct {
	mu     sync.Mutex
	before func(context.Context, string)
}

func (trace *billingQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	before := trace.before
	trace.mu.Unlock()
	if before != nil {
		before(ctx, data.SQL)
	}
	return ctx
}

func (*billingQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *billingQueryTrace) set(before func(context.Context, string)) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.before = before
}

func newBillingProtocolFixture(t *testing.T) *billingProtocolFixture {
	t.Helper()
	databaseURL := os.Getenv("BILLING_DATABASE_URL")
	seeds := os.Getenv("BROKER_SEEDS")
	if databaseURL == "" || seeds == "" {
		t.Skip("BILLING_DATABASE_URL and BROKER_SEEDS are required for the real billing protocol suite")
	}
	adminPool, err := pgxpool.New(t.Context(), databaseURL)
	require.NoError(t, err)
	t.Cleanup(adminPool.Close)
	suffix := strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
	schema := pgx.Identifier{"billing_protocol_" + suffix}.Sanitize()
	_, err = adminPool.Exec(t.Context(), "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_, dropErr := adminPool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		require.NoError(t, dropErr)
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	require.NoError(t, err)
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 3
	trace := &billingQueryTrace{}
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	migrations, err := filepath.Glob("../../db/migrations/*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, migrations)
	for _, path := range migrations {
		body, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		up, _, _ := strings.Cut(string(body), "-- +goose Down")
		_, migrateErr := pool.Exec(t.Context(), up)
		require.NoError(t, migrateErr, path)
	}
	observer := &billingOffsetObserver{fetched: make(chan struct{}, 1)}
	broker, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(seeds, ",")...), kgo.RecordPartitioner(kgo.ManualPartitioner()), kgo.WithHooks(observer))
	require.NoError(t, err)
	t.Cleanup(broker.Close)
	service := NewBillingService(pool, broker, slog.New(slog.DiscardHandler), "billing.events")
	service.consumerName = "billing.protocol." + suffix
	service.teachingTopic = "billing-protocol-" + suffix
	service.now = func() time.Time { return time.Date(2026, time.September, 22, 0, 0, 0, 0, time.UTC) }
	created, err := service.admin.CreateTopics(t.Context(), 2, 1, nil, service.teachingTopic)
	require.NoError(t, err)
	require.NoError(t, created[service.teachingTopic].Err)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_, deleteErr := service.admin.DeleteGroups(ctx, service.consumerName)
		require.NoError(t, deleteErr)
		deleted, deleteErr := service.admin.DeleteTopics(ctx, service.teachingTopic)
		require.NoError(t, deleteErr)
		require.NoError(t, deleted[service.teachingTopic].Err)
	})
	details, err := service.admin.ListTopics(t.Context(), service.teachingTopic)
	require.NoError(t, err)
	identity, _ := json.Marshal(map[string]string{service.teachingTopic: details[service.teachingTopic].ID.String()})
	partitions, _ := json.Marshal(map[string][]int32{service.teachingTopic: {0, 1}})
	offsets, _ := json.Marshal(map[string]map[int32]int64{service.teachingTopic: {0: 0, 1: 0}})
	generation := uuid.Must(uuid.NewV7())
	_, err = pool.Exec(t.Context(), `INSERT INTO consumer_readiness (consumer_name, projection_generation, state)
		VALUES ($1, $2, 'certified')`, service.consumerName, generation)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO consumer_replay_manifests
		(consumer_name, projection_generation, source_topic, topic_identity, partition_set, earliest_offsets,
		captured_end_offsets, completed_offsets, started_at, completed_at, operator_identity)
		VALUES ($1, $2, $3, $4, $5, $6, $6, $6, now(), transaction_timestamp(), 'billing-protocol-test')`,
		service.consumerName, generation, service.teachingTopic, string(identity), partitions, offsets)
	require.NoError(t, err)
	fixture := &billingProtocolFixture{
		service: service, pool: pool, broker: broker, trace: trace, progressRead: observer.fetched,
		tutorID: uuid.Must(uuid.NewV7()), classID: uuid.Must(uuid.NewV7()), period: BillingPeriod{Year: 2026, Month: 8},
	}
	fixture.commit(t, 0, 0)
	_, err = NewProfileService(pool).Save(t.Context(), fixture.tutorID, ProfileInput{
		LegalName: new("Tutor"), ContactLine: new("tutor@example.com"), BankCode: new("970436"),
		BankAccountNumber: new("123456"), BankAccountHolder: new("TUTOR"),
	})
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO classes (class_id, tutor_id, name) VALUES ($1,$2,'Maths')`, fixture.classID, fixture.tutorID)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `INSERT INTO class_rates (class_id,tutor_id,effective_from,rate_amount,currency)
		VALUES ($1,$2,'2026-08-01',250000,'VND')`, fixture.classID, fixture.tutorID)
	require.NoError(t, err)
	return fixture
}

func (fixture *billingProtocolFixture) commit(t *testing.T, first, second int64) {
	t.Helper()
	offsets := kadm.Offsets{}
	for partition, at := range []int64{first, second} {
		offsets.Add(kadm.Offset{Topic: fixture.service.teachingTopic, Partition: int32(partition), At: at, LeaderEpoch: -1})
	}
	require.NoError(t, fixture.service.admin.CommitAllOffsets(t.Context(), fixture.service.consumerName, offsets))
}

func (fixture *billingProtocolFixture) seedCandidates(t *testing.T, students, sessions int) {
	t.Helper()
	for range students {
		studentID := uuid.Must(uuid.NewV7())
		_, err := fixture.pool.Exec(t.Context(), `INSERT INTO students (student_id,tutor_id,name) VALUES ($1,$2,'Mai')`, studentID, fixture.tutorID)
		require.NoError(t, err)
		_, err = fixture.pool.Exec(t.Context(), `INSERT INTO roster_periods (class_id,student_id,tutor_id,effective_from)
			VALUES ($1,$2,$3,'2026-08-01')`, fixture.classID, studentID, fixture.tutorID)
		require.NoError(t, err)
	}
	for index := range sessions {
		sessionID := uuid.Must(uuid.NewV7())
		date := time.Date(2026, time.August, 1+index, 0, 0, 0, 0, time.UTC)
		_, err := fixture.pool.Exec(t.Context(), `INSERT INTO sessions (session_id,class_id,tutor_id,starts_at,ends_at,local_date)
			VALUES ($1,$2,$3,$4,$5,$4::timestamptz::date)`, sessionID, fixture.classID, fixture.tutorID, date, date.Add(time.Hour))
		require.NoError(t, err)
	}
	_, err := fixture.pool.Exec(t.Context(), `INSERT INTO attendance (session_id,student_id,tutor_id,state,marked_at)
		SELECT s.session_id, st.student_id, s.tutor_id, 'Present', now() FROM sessions s JOIN students st USING (tutor_id)
		ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
}

func (fixture *billingProtocolFixture) preview(t *testing.T) BillingPreview {
	t.Helper()
	preview, err := fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
	require.NoError(t, err)
	return preview
}

func (fixture *billingProtocolFixture) noMoney(t *testing.T) {
	t.Helper()
	for _, table := range []string{"billing_runs", "invoices", "invoice_lines", "invoice_number_counters", "outbox"} {
		var count int
		err := fixture.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count)
		require.NoError(t, err)
		require.Zero(t, count, table)
	}
}

// covers: spec 0013 AC-14, AC-15, AC-24
func TestBillingService_IssueRollsBackEveryMoneyWrite(t *testing.T) {
	for _, test := range []struct{ name, table, check string }{
		{"run", "billing_runs", "false"},
		{"number after run", "invoice_number_counters", "false"},
		{"invoice after number", "invoices", "false"},
		{"line after invoice", "invoice_lines", "false"},
		{"outbox after line", "outbox", "false"},
		{"next student after outbox", "invoice_number_counters", "last_sequence < 2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newBillingProtocolFixture(t)
			fixture.seedCandidates(t, 2, 1)
			preview := fixture.preview(t)
			require.Equal(t, "ready", preview.Status)
			_, err := fixture.pool.Exec(t.Context(), "ALTER TABLE "+pgx.Identifier{test.table}.Sanitize()+" ADD CONSTRAINT fail_issue CHECK ("+test.check+")")
			require.NoError(t, err)
			_, _, err = fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{PreviewFingerprint: *preview.Fingerprint})
			require.Error(t, err)
			fixture.noMoney(t)
			_, err = fixture.pool.Exec(t.Context(), "ALTER TABLE "+pgx.Identifier{test.table}.Sanitize()+" DROP CONSTRAINT fail_issue")
			require.NoError(t, err)
			run, status, err := fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{PreviewFingerprint: *preview.Fingerprint})
			require.NoError(t, err)
			require.Equal(t, http.StatusCreated, status)
			require.Equal(t, "2026-0001", run.Invoices[0].InvoiceNumber)
			require.Equal(t, "2026-0002", run.Invoices[1].InvoiceNumber)
		})
	}
}

// covers: spec 0013 AC-16, AC-17, AC-19, AC-24
func TestBillingService_ConcurrentIssueAndAuthoritativeRecovery(t *testing.T) {
	fixture := newBillingProtocolFixture(t)
	fixture.seedCandidates(t, 1, 1)
	preview := fixture.preview(t)
	atInsert := make(chan struct{}, 2)
	proceed := make(chan struct{})
	fixture.trace.set(func(ctx context.Context, query string) {
		if strings.Contains(query, "-- name: InsertBillingRun") {
			atInsert <- struct{}{}
			select {
			case <-proceed:
			case <-ctx.Done():
			}
		}
	})
	type outcome struct {
		run    BillingRunResult
		status int
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			run, status, err := fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{PreviewFingerprint: *preview.Fingerprint})
			results <- outcome{run, status, err}
		}()
	}
	for range 2 {
		select {
		case <-atInsert:
		case <-time.After(10 * time.Second):
			t.Fatal("both issue transactions must reach the run insert")
		}
	}
	close(proceed)
	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusOK}, []int{first.status, second.status})
	require.Equal(t, first.run, second.run)
	fixture.trace.set(nil)
	var outboxCount int
	require.NoError(t, fixture.pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox`).Scan(&outboxCount))
	require.Equal(t, 1, outboxCount)
	_, err := fixture.pool.Exec(t.Context(), `UPDATE classes SET name='Changed'; UPDATE class_rates SET rate_amount=0;
		UPDATE students SET name='Changed'; UPDATE invoice_profiles SET legal_name='Changed',revision=revision+1`)
	require.NoError(t, err)
	unavailable, err := kgo.NewClient()
	require.NoError(t, err)
	unavailable.Close()
	admin := fixture.service.admin
	fixture.service.admin = kadm.NewClient(unavailable)
	t.Cleanup(func() { fixture.service.admin = admin })
	// A lost response is recoverable even when the broker client is unavailable.
	state, err := fixture.service.ReadPeriod(t.Context(), fixture.tutorID, "UTC", fixture.period)
	require.NoError(t, err)
	require.Equal(t, first.run, *state.Run)
	storedPreview, err := fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
	require.NoError(t, err)
	require.Equal(t, "already_issued", storedPreview.Status)
	require.Nil(t, storedPreview.Fingerprint)
	retry, status, err := fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, first.run, retry)
	other, err := fixture.service.ReadPeriod(t.Context(), uuid.Must(uuid.NewV7()), "UTC", fixture.period)
	require.NoError(t, err)
	require.Equal(t, "unissued", other.Status)
}

// covers: spec 0013 AC-7, AC-12, AC-13, AC-24
func TestBillingService_StaleInputsCannotIssue(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"attendance", `UPDATE attendance SET state='Absent'`},
		{"unmarked", `DELETE FROM attendance`},
		{"rate", `UPDATE class_rates SET rate_amount=0`},
		{"missing rate", `DELETE FROM class_rates`},
		{"student label", `UPDATE students SET name='Changed'`},
		{"class label", `UPDATE classes SET name='Changed'`},
		{"session date", `UPDATE sessions SET local_date='2026-08-02'`},
		{"profile", `UPDATE invoice_profiles SET revision=revision+1, legal_name='Changed'`},
		{"profile blocker", `UPDATE invoice_profiles SET bank_account_number=NULL`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newBillingProtocolFixture(t)
			fixture.seedCandidates(t, 1, 1)
			preview := fixture.preview(t)
			_, err := fixture.pool.Exec(t.Context(), test.mutation)
			require.NoError(t, err)
			_, _, err = fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{PreviewFingerprint: *preview.Fingerprint})
			var stale *BillingPeriodError
			require.ErrorAs(t, err, &stale)
			require.Equal(t, "preview_stale", stale.Code)
			fixture.noMoney(t)
		})
	}
}

// covers: spec 0013 AC-6, AC-7, AC-15, AC-20, AC-24
func TestBillingService_SupportsTheExactVolumeBoundAndRefusesGrowth(t *testing.T) {
	fixture := newBillingProtocolFixture(t)
	fixture.seedCandidates(t, 500, 20)
	_, err := fixture.pool.Exec(t.Context(), `UPDATE class_rates SET rate_amount=0`)
	require.NoError(t, err)
	preview := fixture.preview(t)
	require.Equal(t, "ready", preview.Status)
	require.Len(t, preview.Students, 500)
	require.Len(t, preview.Students[499].Lines, 20)
	require.Zero(t, preview.GrandTotal)
	fixture.noMoney(t)
	for _, test := range []struct {
		name               string
		students, sessions int
	}{
		{"candidate limit", 0, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture.seedCandidates(t, test.students, test.sessions)
			_, previewErr := fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
			var oversized *BillingPeriodError
			require.ErrorAs(t, previewErr, &oversized)
			require.Equal(t, "period_too_large", oversized.Code)
			_, _, issueErr := fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{PreviewFingerprint: *preview.Fingerprint})
			var stale *BillingPeriodError
			require.ErrorAs(t, issueErr, &stale)
			require.Equal(t, "preview_stale", stale.Code)
			fixture.noMoney(t)
			if test.sessions > 0 {
				_, err = fixture.pool.Exec(t.Context(), `DELETE FROM sessions WHERE session_id=(SELECT session_id FROM sessions ORDER BY recorded_at DESC LIMIT 1)`)
			} else {
				_, err = fixture.pool.Exec(t.Context(), `DELETE FROM roster_periods WHERE student_id=(SELECT student_id FROM students ORDER BY recorded_at DESC LIMIT 1)`)
			}
			require.NoError(t, err)
		})
	}
	run, status, err := fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{PreviewFingerprint: *preview.Fingerprint})
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, status)
	require.Len(t, run.Invoices, 500)
	require.Len(t, run.Invoices[499].Lines, 20)
	require.Zero(t, run.GrandTotal)
}

// covers: spec 0013 AC-8, AC-24
func TestBillingService_BarrierReturnsItsRetryOutcomeWithoutMoney(t *testing.T) {
	fixture := newBillingProtocolFixture(t)
	fixture.seedCandidates(t, 1, 1)
	require.NoError(t, fixture.broker.ProduceSync(t.Context(), &kgo.Record{
		Topic: fixture.service.teachingTopic, Partition: 1, Value: []byte(`{"pending":true}`),
	}).FirstErr())
	started := time.Now()
	_, err := fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
	var pending *BillingPeriodError
	require.ErrorAs(t, err, &pending)
	require.Equal(t, "projection_sync_pending", pending.Code)
	require.Equal(t, http.StatusServiceUnavailable, pending.Status)
	require.Equal(t, 1, pending.RetryAfter)
	require.GreaterOrEqual(t, time.Since(started), 5*time.Second)
	fixture.noMoney(t)
	fixture.commit(t, 0, 1)
	require.Equal(t, "ready", fixture.preview(t).Status)
}

// covers: spec 0013 AC-12, AC-24
func TestBillingService_PreviewUsesOneRepeatableSnapshot(t *testing.T) {
	fixture := newBillingProtocolFixture(t)
	fixture.seedCandidates(t, 1, 1)
	before := fixture.preview(t)
	readingProfile := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	fixture.trace.set(func(ctx context.Context, query string) {
		if strings.Contains(query, "-- name: GetInvoiceProfile") {
			once.Do(func() { close(readingProfile) })
			select {
			case <-proceed:
			case <-ctx.Done():
			}
		}
	})
	type outcome struct {
		preview BillingPreview
		err     error
	}
	result := make(chan outcome, 1)
	go func() {
		preview, err := fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
		result <- outcome{preview, err}
	}()
	select {
	case <-readingProfile:
	case <-time.After(10 * time.Second):
		t.Fatal("preview must reach the profile read")
	}
	_, err := fixture.pool.Exec(t.Context(), `UPDATE invoice_profiles SET legal_name='Changed',revision=revision+1`)
	require.NoError(t, err)
	close(proceed)
	got := <-result
	require.NoError(t, got.err)
	require.Equal(t, before.Fingerprint, got.preview.Fingerprint)
	fixture.trace.set(nil)
	require.NotEqual(t, before.Fingerprint, fixture.preview(t).Fingerprint)
}

// covers: spec 0013 AC-8, AC-24
func TestBillingService_BarrierKeepsItsCapturedCut(t *testing.T) {
	fixture := newBillingProtocolFixture(t)
	fixture.seedCandidates(t, 1, 1)
	publish := func() {
		require.NoError(t, fixture.broker.ProduceSync(t.Context(), &kgo.Record{
			Topic: fixture.service.teachingTopic, Partition: 1, Value: []byte(`{"marker":true}`),
		}).FirstErr())
	}
	publish()
	type outcome struct {
		preview BillingPreview
		err     error
	}
	result := make(chan outcome, 1)
	go func() {
		preview, err := fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
		result <- outcome{preview, err}
	}()
	select {
	case <-fixture.progressRead:
	case <-time.After(10 * time.Second):
		t.Fatal("barrier must poll its captured cut")
	}
	publish()
	fixture.commit(t, 0, 1)
	got := <-result
	require.NoError(t, got.err)
	require.Equal(t, "ready", got.preview.Status)
	ends, err := fixture.service.admin.ListEndOffsets(t.Context(), fixture.service.teachingTopic)
	require.NoError(t, err)
	require.Equal(t, int64(2), ends[fixture.service.teachingTopic][1].Offset)
	fixture.noMoney(t)
}

// covers: spec 0013 AC-20, AC-24
func TestBillingService_StudentLimitAppliesBelowTheCandidateLimit(t *testing.T) {
	fixture := newBillingProtocolFixture(t)
	fixture.seedCandidates(t, 500, 1)
	preview := fixture.preview(t)
	require.Len(t, preview.Students, 500)
	fixture.seedCandidates(t, 1, 0)
	_, err := fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
	var oversized *BillingPeriodError
	require.ErrorAs(t, err, &oversized)
	require.Equal(t, "period_too_large", oversized.Code)
	require.Contains(t, oversized.Message, "500 student")
	_, _, err = fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{PreviewFingerprint: *preview.Fingerprint})
	var stale *BillingPeriodError
	require.ErrorAs(t, err, &stale)
	require.Equal(t, "preview_stale", stale.Code)
	fixture.noMoney(t)
}

// covers: spec 0013 AC-7, AC-9, AC-11, AC-24
func TestBillingService_ProjectionFailuresAndCalculationBlockers(t *testing.T) {
	for _, test := range []struct{ name, mutation, code, status string }{
		{"missing student", `DELETE FROM students`, "projection_incomplete", ""},
		{"missing class", `DELETE FROM classes`, "projection_incomplete", ""},
		{"unmarked attendance", `DELETE FROM attendance`, "attendance_incomplete", "blocked"},
		{"missing rate", `DELETE FROM class_rates`, "rate_missing", "blocked"},
		{"incomplete profile", `UPDATE invoice_profiles SET legal_name=NULL`, "profile_incomplete", "blocked"},
		{"absent ignores profile", `UPDATE attendance SET state='Absent'; UPDATE invoice_profiles SET legal_name=NULL`, "", "empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newBillingProtocolFixture(t)
			fixture.seedCandidates(t, 1, 1)
			_, err := fixture.pool.Exec(t.Context(), test.mutation)
			require.NoError(t, err)
			preview, err := fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
			if test.status == "" {
				var incomplete *BillingPeriodError
				require.ErrorAs(t, err, &incomplete)
				require.Equal(t, test.code, incomplete.Code)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.status, preview.Status)
				require.Nil(t, preview.Fingerprint)
				if test.code != "" {
					require.Equal(t, test.code, preview.Blockers[0].Code)
				}
			}
			fixture.noMoney(t)
		})
	}
}

// covers: spec 0013 AC-11, AC-24
func TestBillingService_MissingClassWithoutRosterCannotDisappear(t *testing.T) {
	fixture := newBillingProtocolFixture(t)
	fixture.seedCandidates(t, 0, 1)
	_, err := fixture.pool.Exec(t.Context(), `DELETE FROM classes`)
	require.NoError(t, err)
	_, err = fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
	var incomplete *BillingPeriodError
	require.ErrorAs(t, err, &incomplete)
	require.Equal(t, "projection_incomplete", incomplete.Code)
	fixture.noMoney(t)
}

// covers: spec 0013 AC-9, AC-13, AC-24
func TestBillingService_UnresolvedFailureScopeAndPrecedence(t *testing.T) {
	fixture := newBillingProtocolFixture(t)
	fixture.seedCandidates(t, 1, 1)
	preview := fixture.preview(t)
	_, err := fixture.pool.Exec(t.Context(), `INSERT INTO consumer_failures
		(consumer_name,source_topic,source_partition,source_offset,tutor_id,failure_category)
		VALUES ($1,$2,0,0,$3,'handler_failed')`, fixture.service.consumerName, fixture.service.teachingTopic, uuid.Must(uuid.NewV7()))
	require.NoError(t, err)
	require.Equal(t, "ready", fixture.preview(t).Status)
	for _, tutorID := range []*uuid.UUID{&fixture.tutorID, nil} {
		_, err = fixture.pool.Exec(t.Context(), `UPDATE consumer_failures SET tutor_id=$1`, tutorID)
		require.NoError(t, err)
		_, err = fixture.service.Preview(t.Context(), fixture.tutorID, "UTC", fixture.period)
		var failed *BillingPeriodError
		require.ErrorAs(t, err, &failed)
		require.Equal(t, "projection_failed", failed.Code)
		_, _, err = fixture.service.Issue(t.Context(), fixture.tutorID, "UTC", fixture.period, IssueBillingInput{PreviewFingerprint: "stale"})
		require.ErrorAs(t, err, &failed)
		require.Equal(t, "projection_failed", failed.Code)
		fixture.noMoney(t)
	}
	_, err = fixture.pool.Exec(t.Context(), `UPDATE consumer_failures SET resolved_at=now(),resolution='replayed'`)
	require.NoError(t, err)
	require.Equal(t, preview.Fingerprint, fixture.preview(t).Fingerprint)
}
