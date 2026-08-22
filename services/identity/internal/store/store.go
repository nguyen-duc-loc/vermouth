// Package store reaches identity's own database and nothing else (STK-5,
// INV-2). The typed query methods in the sqlcgen subpackage are generated from
// db/queries against db/migrations by `task generate`.
package store

import "github.com/nguyen-duc-loc/vermouth/services/identity/internal/store/sqlcgen"

// Queries binds the generated queries to a pool, a connection or a
// transaction, so a handler can run them inside the transaction that also
// writes the outbox (STK-4).
func Queries(db sqlcgen.DBTX) *sqlcgen.Queries { return sqlcgen.New(db) }

// Tutor is one row of identity's own tutors table, named here so a handler can
// pass a row around without importing the generated package itself.
type Tutor = sqlcgen.Tutor
