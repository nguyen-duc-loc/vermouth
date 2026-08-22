package model_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// services are the four bounded contexts, each with its own database, its own
// migrations and its own hand written SQL (INV-2, STK-5).
var services = []string{"identity", "teaching", "billing", "notifications"}

// repoRoot is two directories up from this package, which is where the service
// tree lives. The guards read the repository rather than a copy of it, so a
// query added tomorrow is guarded without anyone remembering to list it here.
const repoRoot = "../.."

// kitTables belong to the shared module and are reached with pgx directly, never
// through a service's db/queries (STK-3). They are owned tables all the same, so
// they are excluded by name rather than by absence.
var kitTables = map[string]bool{"outbox": true, "handled_events": true, "goose_db_version": true}

// commentLine strips a whole line SQL comment, so prose above a statement cannot
// be mistaken for the statement's own SQL.
var commentLine = regexp.MustCompile(`(?m)^\s*--.*$`)

// tablePosition captures the identifier in every position where SQL names a
// table: what is written to, what is read from, and what is joined.
var tablePosition = regexp.MustCompile(`(?is)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM|FROM|JOIN)\s+([a-z_][a-z0-9_]*)`)

// statementName captures sqlc's own statement header, so a failure names the
// query a reader can go and find.
var statementName = regexp.MustCompile(`--\s*name:\s*(\w+)\s*:(\w+)`)

// ownedTables reads the tables a service owns out of that service's own
// migrations. The owned set is therefore whatever the migrations say, not a list
// in a test that can quietly fall behind them.
func ownedTables(t *testing.T, service string) map[string]bool {
	t.Helper()
	createTable := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+([a-z_][a-z0-9_]*)`)
	owned := make(map[string]bool)
	dir := filepath.Join(repoRoot, "services", service, "db", "migrations")
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "%s has no migrations", service)
	for _, file := range files {
		body, err := os.ReadFile(file)
		require.NoError(t, err)
		for _, match := range createTable.FindAllStringSubmatch(string(body), -1) {
			owned[match[1]] = true
		}
	}
	return owned
}

// statement is one sqlc statement: its name, its kind, and its SQL with the
// comments taken out.
type statement struct {
	file string
	name string
	kind string
	sql  string
}

// readStatements splits a service's db/queries into statements the way sqlc
// does, on the -- name: header.
func readStatements(t *testing.T, service string) []statement {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(repoRoot, "services", service, "db", "queries", "*.sql"))
	require.NoError(t, err)
	var statements []statement
	for _, file := range files {
		body, err := os.ReadFile(file)
		require.NoError(t, err)
		headers := statementName.FindAllSubmatchIndex(body, -1)
		for i, header := range headers {
			end := len(body)
			if i+1 < len(headers) {
				end = headers[i+1][0]
			}
			text := string(body[header[1]:end])
			statements = append(statements, statement{
				file: filepath.Base(file),
				name: string(body[header[2]:header[3]]),
				kind: string(body[header[4]:header[5]]),
				sql:  commentLine.ReplaceAllString(text, ""),
			})
		}
	}
	return statements
}

// TestQueriesNameOnlyTablesTheirServiceOwns is the ownership guard (AC-6). A
// statement reaching a table its service does not own would be the one failure
// this architecture cannot recover from, and it is invisible in review once
// there are four schemas to remember.
func TestQueriesNameOnlyTablesTheirServiceOwns(t *testing.T) {
	t.Parallel()
	total := 0
	for _, service := range services {
		owned := ownedTables(t, service)
		for _, s := range readStatements(t, service) {
			total++
			for _, match := range tablePosition.FindAllStringSubmatch(s.sql, -1) {
				table := strings.ToLower(match[1])
				// Two positions name no table: LEFT JOIN LATERAL ( opens a
				// subquery, and ON CONFLICT DO UPDATE SET writes back into the
				// row the insert already named.
				if table == "lateral" || table == "set" {
					continue
				}
				require.True(t, owned[table],
					"%s/%s names table %q, which %s does not own (INV-2, AC-6)",
					service, s.name, table, service)
				require.False(t, kitTables[table],
					"%s/%s queries the shared module's %q table, which is reached with pgx directly (STK-3)",
					service, s.name, table)
			}
		}
	}
	require.NotZero(t, total, "found no statements to guard")
}

// exemption is one statement allowed to name no tutor_id: why it cannot, and
// the column it identifies its row by instead.
type exemption struct {
	reason       string
	identifiedBy string
}

// beforeATokenExists are the only statements that cannot filter by tutor_id,
// with the reason each is allowed. Sign in is what produces a tutor_id, so
// everything on the way to one has none to filter by: it matches on identity's
// unique email, on Google's subject, or on an unguessable single use value from
// crypto/rand (spec 0004). The exemption is deliberately a named list rather
// than a rule: adding to it is a visible decision, and the test still insists
// each one names the column it identifies a single row by.
var beforeATokenExists = map[string]exemption{
	"identity/GetTutorByEmail": {
		reason:       "sign in, which has no token yet, so it matches on the unique email",
		identifiedBy: "email = ",
	},
	"identity/GetTutorByProviderSubject": {
		reason:       "the callback resolves the tutor from Google's subject, which is the identity an email only copies",
		identifiedBy: "provider_subject = ",
	},
	"identity/InsertLoginAttempt": {
		reason:       "an in flight sign in exists before the tutor does, so there is no tutor_id to store",
		identifiedBy: "state",
	},
	"identity/GetLoginAttempt": {
		reason:       "the callback reads its own attempt by the single use state it was handed, before any tutor is known",
		identifiedBy: "state = ",
	},
	"identity/ConsumeLoginAttempt": {
		reason:       "the callback claims its own attempt by the single use state it was handed",
		identifiedBy: "state = ",
	},
	"identity/GetRefreshToken": {
		reason:       "a refresh is presented before any token names a tutor, so the cookie's own hash is the only handle",
		identifiedBy: "token_hash = ",
	},
	"identity/MarkRefreshTokenUsed": {
		reason:       "the same, and rotation has to be one statement so two tabs racing cannot both win it",
		identifiedBy: "token_hash = ",
	},
}

// sweeps delete rows that have already expired or been revoked. They cross every
// tutor on purpose, which is why each one must name the column that says the row
// is finished: a sweep able to match a live row would be data loss wearing a
// housekeeping name.
var sweeps = map[string]string{
	"identity/DeleteExpiredLoginAttempts":  "expires_at",
	"identity/DeleteFinishedRefreshTokens": "expires_at",
}

// TestEveryStatementNamesTutorID is the tenancy guard (AC-4, INV-8). Every table
// in the model carries tutor_id, so every statement must name it: a read filters
// on it, and a write stores it. tutor_id comes from the sub claim of the verified
// token and never from an input, which is what makes one tutor unable to reach
// another's rows.
func TestEveryStatementNamesTutorID(t *testing.T) {
	t.Parallel()
	for _, service := range services {
		for _, s := range readStatements(t, service) {
			key := service + "/" + s.name
			upper := strings.ToUpper(s.sql)
			switch {
			case strings.HasPrefix(strings.TrimSpace(upper), "INSERT"):
				// An insert stores the owner rather than filtering on it, so
				// tutor_id has to be among the columns it writes.
				columns := s.sql[strings.Index(s.sql, "(")+1 : strings.Index(s.sql, ")")]
				if allowed, exempt := beforeATokenExists[key]; exempt {
					require.Contains(t, columns, allowed.identifiedBy,
						"%s/%s stores no tutor_id as %s, so it must store %s instead",
						service, s.name, allowed.reason, allowed.identifiedBy)
					continue
				}
				require.Contains(t, columns, "tutor_id",
					"%s/%s inserts without storing tutor_id (AC-4, INV-8)", service, s.name)
			default:
				where := strings.LastIndex(upper, "WHERE")
				if finished, isSweep := sweeps[key]; isSweep {
					require.NotEqual(t, -1, where,
						"%s/%s is a sweep with no WHERE, so it would delete live rows", service, s.name)
					require.Contains(t, s.sql[where:], finished,
						"%s/%s is a sweep, so it must name %s to prove it only touches finished rows",
						service, s.name, finished)
					continue
				}
				if allowed, exempt := beforeATokenExists[key]; exempt {
					require.Contains(t, s.sql, allowed.identifiedBy,
						"%s/%s is exempt from the tutor_id filter as %s, so it must name %s",
						service, s.name, allowed.reason, allowed.identifiedBy)
					continue
				}
				require.NotEqual(t, -1, where,
					"%s/%s reads or writes without a WHERE, so it cannot be scoped to one tutor (AC-4)",
					service, s.name)
				require.Contains(t, s.sql[where:], "tutor_id",
					"%s/%s does not name tutor_id in its WHERE (AC-4, INV-8)", service, s.name)
			}
		}
	}
}

// TestBothServicesUseOneCoveragePredicate holds the one thing two services
// counting the same membership independently could disagree about: where a roster
// period starts and ends. Spec 0003 fixes one predicate, inclusive at both ends,
// and this asserts billing and notifications carry it character for character
// rather than each having picked a boundary for itself.
func TestBothServicesUseOneCoveragePredicate(t *testing.T) {
	t.Parallel()
	const predicate = "rp.effective_from <= s.local_date AND (rp.effective_to IS NULL OR s.local_date <= rp.effective_to)"
	whitespace := regexp.MustCompile(`\s+`)
	for _, service := range []string{"billing", "notifications"} {
		found := false
		for _, s := range readStatements(t, service) {
			flat := whitespace.ReplaceAllString(s.sql, " ")
			if strings.Contains(flat, predicate) {
				found = true
			}
		}
		require.True(t, found,
			"%s does not carry spec 0003's one roster coverage predicate verbatim: %s", service, predicate)
	}
}

// TestProjectionCommentsNameTheirEvents is the readability half of AC-3: a
// projection table is only safe to replay if the events that write it are named
// where the table is defined, so a wrong consumer is visible while reading the
// schema rather than only while reading Go.
func TestProjectionCommentsNameTheirEvents(t *testing.T) {
	t.Parallel()
	projections := map[string][]string{
		"billing":       {"students", "classes", "sessions", "attendance", "roster_periods", "class_rates"},
		"notifications": {"recipients", "classes", "sessions", "roster_periods"},
	}
	for service, tables := range projections {
		files, err := filepath.Glob(filepath.Join(repoRoot, "services", service, "db", "migrations", "*.sql"))
		require.NoError(t, err)
		schema := ""
		for _, file := range files {
			body, err := os.ReadFile(file)
			require.NoError(t, err)
			schema += string(body)
		}
		for _, table := range tables {
			at := regexp.MustCompile(`(?i)CREATE\s+TABLE\s+` + table + `\b`).FindStringIndex(schema)
			require.NotNil(t, at, "%s has no CREATE TABLE %s", service, table)
			comment := commentBlockAbove(schema[:at[0]])
			require.Regexp(t, `(identity|teaching)\.[a-z.]+`, comment,
				"the comment above %s.%s names no event that writes it (AC-3)", service, table)
		}
	}
}

// commentBlockAbove returns the run of comment lines immediately above a
// definition, which is where this repository keeps the why.
func commentBlockAbove(before string) string {
	lines := strings.Split(strings.TrimRight(before, "\n"), "\n")
	block := make([]string, 0, len(lines))
	for _, line := range slices.Backward(lines) {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			break
		}
		block = append(block, trimmed)
	}
	slices.Reverse(block)
	return strings.Join(block, "\n")
}
