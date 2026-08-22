// Package store reaches billing's own database and nothing else (STK-5,
// INV-2). The typed query methods in the sqlcgen subpackage are generated from
// db/queries against db/migrations by `task generate`.
package store

import "github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"

// Queries binds the generated queries to a pool, a connection or a
// transaction, so the month end run writes every invoice and its outbox row in
// one transaction, and a consumer writes its projection inside the transaction
// that also records the event as handled (STK-4, INV-5).
func Queries(db sqlcgen.DBTX) *sqlcgen.Queries { return sqlcgen.New(db) }

// The rows of billing's authoritative tables, named here so a handler can pass
// one around without importing the generated package itself. The projections
// (students, classes, sessions, attendance, roster periods, class rates) are
// deliberately absent: only internal/consumer writes those, and only from an
// event.
type (
	// InvoiceProfile is the tutor's own profile and bank details, carrying
	// is_complete, the one completeness gate the month end refusal and the
	// profile screen both read.
	InvoiceProfile = sqlcgen.InvoiceProfile
	// Invoice is one issued invoice, including the render block frozen onto
	// it at issue.
	Invoice = sqlcgen.Invoice
	// InvoiceLine is one billable session on an invoice, at the money it was
	// issued with.
	InvoiceLine = sqlcgen.InvoiceLine
	// BillingRun is one month end run for one tutor, period and generation.
	BillingRun = sqlcgen.BillingRun
)
