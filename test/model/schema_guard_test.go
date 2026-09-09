package model_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// connect opens one service's own database, the way that service does and
// nobody else does (STK-5). A missing URL skips rather than fails: the guards
// need the real Postgres of test/compose.test.yaml (STK-15), and saying so is
// more useful than a red suite on a machine where it is not up.
func connect(t *testing.T, service string) *pgx.Conn {
	t.Helper()
	name := strings.ToUpper(service) + "_DATABASE_URL"
	url := os.Getenv(name)
	if url == "" {
		t.Skipf("%s is not set: run task infra:up and task migrate:up, then task test", name)
	}
	conn, err := pgx.Connect(t.Context(), url)
	require.NoError(t, err, "%s is set but %s did not answer", name, service)
	// The close runs in Cleanup, which Go reaches after t.Context() is already
	// cancelled, so it needs one that outlives the test.
	t.Cleanup(func() { _ = conn.Close(teardownContext()) })
	return conn
}

// teardownContext outlives a test's own context, which Go cancels before it runs
// the Cleanup functions. It takes no *testing.T on purpose: a teardown cannot use
// the context the test just lost.
func teardownContext() context.Context { return context.Background() }

// column is one column of the live schema, read back from information_schema so
// the guard checks what Postgres actually holds rather than what the migration
// meant.
type column struct {
	table     string
	name      string
	dataType  string
	nullable  bool
	generated bool
}

func liveColumns(t *testing.T, conn *pgx.Conn) []column {
	t.Helper()
	rows, err := conn.Query(t.Context(), `
		SELECT table_name, column_name, data_type,
		       is_nullable = 'YES', is_generated = 'ALWAYS'
		FROM information_schema.columns
		WHERE table_schema = 'public'
		ORDER BY table_name, ordinal_position`)
	require.NoError(t, err)
	defer rows.Close()
	var columns []column
	for rows.Next() {
		var c column
		require.NoError(t, rows.Scan(&c.table, &c.name, &c.dataType, &c.nullable, &c.generated))
		columns = append(columns, c)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, columns)
	return columns
}

// TestMoneyIsBigint holds STK-7 and AC-10 in the schema. Money is int64 dong in
// Go and bigint in the database, never a float and never a numeric, and the
// place that mistake gets made is a migration nobody rereads.
func TestMoneyIsBigint(t *testing.T) {
	t.Parallel()
	for _, service := range services {
		conn := connect(t, service)
		for _, c := range liveColumns(t, conn) {
			if c.name == "amount" || strings.HasSuffix(c.name, "_amount") {
				require.Equal(t, "bigint", c.dataType,
					"%s.%s.%s holds money and must be bigint dong (STK-7, AC-10)", service, c.table, c.name)
			}
		}
	}
}

// TestCurrencyIsCheckedVND holds the other half of STK-7: VND only, said by the
// database rather than only by Go.
func TestCurrencyIsCheckedVND(t *testing.T) {
	t.Parallel()
	for _, service := range services {
		conn := connect(t, service)
		checks := make(map[string]string)
		rows, err := conn.Query(t.Context(), `
			SELECT conrelid::regclass::text, string_agg(pg_get_constraintdef(oid), ' ')
			FROM pg_constraint
			WHERE contype = 'c' AND connamespace = 'public'::regnamespace
			GROUP BY conrelid`)
		require.NoError(t, err)
		for rows.Next() {
			var table, defs string
			require.NoError(t, rows.Scan(&table, &defs))
			checks[table] = defs
		}
		rows.Close()
		require.NoError(t, rows.Err())

		for _, c := range liveColumns(t, conn) {
			if c.name == "currency" {
				require.Contains(t, checks[c.table], "'VND'",
					"%s.%s has a currency column with no VND check (STK-7, AC-10)", service, c.table)
			}
		}
	}
}

// TestInstantsAreTimestamptzAndDaysAreDate holds the other half of AC-10. The
// two are worth guarding together because the bug is always the same one: a
// calendar day stored as an instant, which then depends on a timezone nobody
// meant to involve.
func TestInstantsAreTimestamptzAndDaysAreDate(t *testing.T) {
	t.Parallel()
	days := map[string]bool{
		"local_date": true, "session_date": true,
		"effective_from": true, "effective_to": true, "rate_effective_from": true,
	}
	for _, service := range services {
		conn := connect(t, service)
		for _, c := range liveColumns(t, conn) {
			switch {
			case strings.HasSuffix(c.name, "_at"):
				require.Equal(t, "timestamp with time zone", c.dataType,
					"%s.%s.%s is an instant and must be timestamptz in UTC (AC-10)", service, c.table, c.name)
			case days[c.name]:
				require.Equal(t, "date", c.dataType,
					"%s.%s.%s is a calendar day and must be date, not an instant (AC-10)", service, c.table, c.name)
			}
		}
	}
}

// TestEveryTableCarriesTutorID holds INV-8 in the schema (AC-4): a table with no
// tutor_id has no way to be filtered by one, so the tenancy guard on the queries
// would have nothing to stand on.
func TestEveryTableCarriesTutorID(t *testing.T) {
	t.Parallel()
	for _, service := range services {
		conn := connect(t, service)
		hasTutorID := make(map[string]bool)
		tables := make(map[string]bool)
		for _, c := range liveColumns(t, conn) {
			tables[c.table] = true
			if c.name == "tutor_id" {
				hasTutorID[c.table] = true
			}
		}
		for table := range tables {
			// handled_events and goose_db_version are the shared module's and
			// goose's own machinery, keyed by a consumer name and an event id:
			// they hold no tutor's data to scope. login_attempts is identity's
			// own exceptions (spec 0004): an in flight sign in exists before the
			// tutor does, while a refresh token reaches its tutor through the
			// auth_sessions foreign key. Both use unguessable values from
			// crypto/rand, and the tenancy guard on the queries names every access.
			if table == "handled_events" || table == "goose_db_version" || table == "login_attempts" ||
				(service == "identity" && table == "refresh_tokens") {
				continue
			}
			require.True(t, hasTutorID[table],
				"%s.%s carries no tutor_id, so nothing can scope it to one tutor (AC-4, INV-8)", service, table)
		}
	}
}

// primaryKeys reads each table's primary key columns in key order.
func primaryKeys(t *testing.T, conn *pgx.Conn) map[string]string {
	t.Helper()
	rows, err := conn.Query(t.Context(), `
		SELECT c.conrelid::regclass::text,
		       string_agg(a.attname, ',' ORDER BY k.ord)
		FROM pg_constraint c
		CROSS JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		WHERE c.contype = 'p' AND c.connamespace = 'public'::regnamespace
		GROUP BY c.conrelid`)
	require.NoError(t, err)
	defer rows.Close()
	keys := make(map[string]string)
	for rows.Next() {
		var table, cols string
		require.NoError(t, rows.Scan(&table, &cols))
		keys[table] = cols
	}
	require.NoError(t, rows.Err())
	return keys
}

// TestProjectionKeysMatchTheirEventKey is the guard behind AC-7, and the reason
// idempotency needs no care in a consumer. Every projection table is keyed by the
// identifying field its own events already carry, so a redelivered or replayed
// event upserts the same row by construction. A surrogate id here would make a
// replay produce a second row and nothing would say so.
func TestProjectionKeysMatchTheirEventKey(t *testing.T) {
	t.Parallel()
	expected := map[string]map[string]string{
		"billing": {
			"students":       "student_id",
			"classes":        "class_id",
			"sessions":       "session_id",
			"attendance":     "session_id,student_id",
			"roster_periods": "class_id,student_id,effective_from",
			"class_rates":    "class_id,effective_from",
		},
		"notifications": {
			"recipients":     "tutor_id",
			"classes":        "class_id",
			"sessions":       "session_id",
			"roster_periods": "class_id,student_id,effective_from",
		},
	}
	for service, tables := range expected {
		keys := primaryKeys(t, connect(t, service))
		for table, key := range tables {
			require.Equal(t, key, keys[table],
				"%s.%s must be keyed by what its events carry, so a replay lands on the same row (AC-7)",
				service, table)
		}
	}
}

func TestProjectionBookkeepingTimestampsAreComplete(t *testing.T) {
	t.Parallel()
	projections := map[string][]string{
		"billing": {
			"students", "classes", "sessions", "attendance", "roster_periods", "class_rates",
		},
		"notifications": {"recipients", "classes", "sessions", "roster_periods"},
	}
	for service, tables := range projections {
		columns := liveColumns(t, connect(t, service))
		found := make(map[string]map[string]bool, len(tables))
		for _, table := range tables {
			found[table] = make(map[string]bool)
		}
		for _, c := range columns {
			if _, ok := found[c.table]; ok {
				found[c.table][c.name] = true
			}
		}
		for _, table := range tables {
			require.True(t, found[table]["recorded_at"],
				"%s.%s has no first insert bookkeeping timestamp (AC-3)", service, table)
			require.True(t, found[table]["updated_at"],
				"%s.%s has no applied upsert bookkeeping timestamp (AC-3)", service, table)
		}
	}
}

// TestAuthoritativeKeysAreWhatTheInvariantsNeed pins the keys the money path
// leans on: one digest per tutor per day, one counter per tutor and year, and one
// run per tutor, period and generation, which is what makes a double press safe
// rather than the lookup above it.
func TestAuthoritativeKeysAreWhatTheInvariantsNeed(t *testing.T) {
	t.Parallel()
	billing := connect(t, "billing")
	keys := primaryKeys(t, billing)
	require.Equal(t, "tutor_id", keys["invoice_profiles"])
	require.Equal(t, "tutor_id,period_year", keys["invoice_number_counters"])
	require.Equal(t, "invoice_id", keys["invoices"])
	require.Equal(t, "invoice_line_id", keys["invoice_lines"])

	var runUnique, invoiceUnique string
	err := billing.QueryRow(t.Context(), `
		SELECT string_agg(pg_get_constraintdef(oid), ' ')
		FROM pg_constraint WHERE contype = 'u' AND conrelid = 'billing_runs'::regclass`).Scan(&runUnique)
	require.NoError(t, err)
	require.Contains(t, runUnique, "(tutor_id, period_year, period_month, generation)",
		"the unique constraint on billing_runs is what decides a concurrent run, not the lookup above it")
	err = billing.QueryRow(t.Context(), `
		SELECT string_agg(pg_get_constraintdef(oid), ' ')
		FROM pg_constraint WHERE contype = 'u' AND conrelid = 'invoices'::regclass`).Scan(&invoiceUnique)
	require.NoError(t, err)
	require.Contains(t, invoiceUnique, "(tutor_id, invoice_number)",
		"an invoice number is never reused, including by a voided invoice")

	require.Equal(t, "tutor_id,local_date", primaryKeys(t, connect(t, "notifications"))["digest_runs"],
		"one digest per tutor per day is the key, which is also the scheduler's lock (STK-23)")
}

// TestProjectionsCarryNoForeignKey holds AC-6 where it is easy to get wrong.
// Events for two different keys may arrive in either order, so a session may land
// before the class it names: a foreign key on a projection would reject the fact
// rather than record it. Inside a context, between two authoritative tables, a
// foreign key is right and is used.
func TestProjectionsCarryNoForeignKey(t *testing.T) {
	t.Parallel()
	projections := map[string][]string{
		"billing":       {"students", "classes", "sessions", "attendance", "roster_periods", "class_rates"},
		"notifications": {"recipients", "classes", "sessions", "roster_periods"},
	}
	for service, tables := range projections {
		conn := connect(t, service)
		for _, table := range tables {
			var count int
			err := conn.QueryRow(t.Context(), `
				SELECT count(*) FROM pg_constraint
				WHERE contype = 'f' AND conrelid = $1::regclass`, table).Scan(&count)
			require.NoError(t, err)
			require.Zero(t, count,
				"%s.%s is a projection and must reference other contexts by id only (INV-2, AC-6)",
				service, table)
		}
	}
}

// TestNothingIsDeleted holds AC-11 in the schema: everything that can end has a
// nullable end timestamp to end with, rather than a row that disappears and takes
// a past invoice's evidence with it.
func TestNothingIsDeleted(t *testing.T) {
	t.Parallel()
	ends := map[string]map[string]string{
		"teaching": {
			"students": "removed_at", "classes": "archived_at",
			"sessions": "cancelled_at", "roster_periods": "effective_to",
		},
		"billing": {
			"students": "removed_at", "sessions": "cancelled_at",
			"roster_periods": "effective_to", "invoices": "voided_at",
			"billing_runs": "superseded_at",
		},
		"notifications": {"sessions": "cancelled_at", "roster_periods": "effective_to"},
	}
	for service, tables := range ends {
		columns := liveColumns(t, connect(t, service))
		for table, end := range tables {
			found := false
			for _, c := range columns {
				if c.table == table && c.name == end {
					found = true
					require.True(t, c.nullable,
						"%s.%s.%s must be nullable: empty is what not yet ended means (AC-11)",
						service, table, end)
				}
			}
			require.True(t, found, "%s.%s has no %s to end with (AC-11)", service, table, end)
		}
	}
}

// TestCompletenessGateIsGenerated holds AC-12. The gate is one generated column
// so Postgres recomputes it from the same row: the month end refusal and the
// profile screen read the same answer, and neither can hold a stale copy.
func TestCompletenessGateIsGenerated(t *testing.T) {
	t.Parallel()
	found := false
	for _, c := range liveColumns(t, connect(t, "billing")) {
		if c.table == "invoice_profiles" && c.name == "is_complete" {
			found = true
			require.True(t, c.generated, "is_complete must be a generated column, not a stored flag (AC-12)")
			require.False(t, c.nullable, "is_complete answers yes or no, never unknown")
			require.Equal(t, "boolean", c.dataType)
		}
	}
	require.True(t, found, "billing.invoice_profiles has no is_complete gate (AC-12)")
}

// TestOneOwningTablePerEntity is AC-1 read against the live schema: exactly the
// tables spec 0003 places in each service, no more and no fewer, so a second home
// for an entity cannot appear unnoticed.
func TestOneOwningTablePerEntity(t *testing.T) {
	t.Parallel()
	expected := map[string][]string{
		// identity's four extra tables are spec 0004's, not spec 0003's: the
		// Google account link, the in flight sign in, the locked session family,
		// and its hashed refresh tokens. None is a second home for an entity.
		"identity": {"tutors", "tutor_identities", "login_attempts", "auth_sessions", "refresh_tokens"},
		// command_receipts belongs to spec 0009's create retry contract. It owns
		// command identity, not a second copy of a domain entity.
		"teaching": {
			"students", "classes", "sessions", "roster_periods", "attendance", "command_receipts",
		},
		"billing": {
			"students", "classes", "sessions", "attendance", "roster_periods", "class_rates",
			"invoice_profiles", "invoice_number_counters", "billing_runs", "invoices", "invoice_lines",
		},
		"notifications": {"recipients", "classes", "sessions", "roster_periods", "digest_runs"},
	}
	for service, tables := range expected {
		conn := connect(t, service)
		live := make(map[string]bool)
		for _, c := range liveColumns(t, conn) {
			if !kitTables[c.table] {
				live[c.table] = true
			}
		}
		for _, table := range tables {
			require.True(t, live[table], "%s is missing its %s table (AC-1)", service, table)
			delete(live, table)
		}
		require.Empty(t, live, "%s holds tables spec 0003 does not place there (AC-1)", service)
	}
}

// TestIdentityAuthenticationSchemaKeepsOnlyHashedSecretsAndOwnedLinks proves
// the durable database boundary for browser sign in.
// covers: AC-8, AC-9, AC-11, AC-12
func TestIdentityAuthenticationSchemaKeepsOnlyHashedSecretsAndOwnedLinks(t *testing.T) {
	t.Parallel()
	identity := connect(t, "identity")

	var tutorChecks string
	err := identity.QueryRow(t.Context(), `
		SELECT string_agg(pg_get_constraintdef(oid), ' ')
		FROM pg_constraint
		WHERE contype = 'c' AND conrelid IN (
			'tutors'::regclass,
			'tutor_identities'::regclass
		)`).Scan(&tutorChecks)
	require.NoError(t, err)
	require.Contains(t, tutorChecks, "lower(btrim(email))")
	require.Contains(t, tutorChecks, "lower(btrim(provider_email))")

	var foreignKeys string
	err = identity.QueryRow(t.Context(), `
		SELECT string_agg(pg_get_constraintdef(oid), ' ')
		FROM pg_constraint
		WHERE contype = 'f' AND conrelid IN (
			'auth_sessions'::regclass,
			'refresh_tokens'::regclass
		)`).Scan(&foreignKeys)
	require.NoError(t, err)
	require.Contains(t, foreignKeys, "FOREIGN KEY (tutor_id) REFERENCES tutors(tutor_id) ON DELETE CASCADE")
	require.Contains(t, foreignKeys, "FOREIGN KEY (session_id) REFERENCES auth_sessions(session_id) ON DELETE CASCADE")

	var browserBindingType, refreshHashType string
	err = identity.QueryRow(t.Context(), `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'login_attempts'
		  AND column_name = 'browser_binding_hash'`).Scan(&browserBindingType)
	require.NoError(t, err)
	err = identity.QueryRow(t.Context(), `
		SELECT data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'refresh_tokens'
		  AND column_name = 'token_hash'`).Scan(&refreshHashType)
	require.NoError(t, err)
	require.Equal(t, "bytea", browserBindingType)
	require.Equal(t, "bytea", refreshHashType)

	var rawSecretColumns int
	err = identity.QueryRow(t.Context(), `
		SELECT count(*)
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND (
			column_name ILIKE '%password%'
			OR column_name IN ('browser_binding', 'refresh_token')
		  )`).Scan(&rawSecretColumns)
	require.NoError(t, err)
	require.Zero(t, rawSecretColumns)
}
