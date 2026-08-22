// Package store reaches teaching's own database and nothing else (STK-5,
// INV-2). The typed query methods in the sqlcgen subpackage are generated from
// db/queries against db/migrations by `task generate`.
package store

import "github.com/nguyen-duc-loc/vermouth/services/teaching/internal/store/sqlcgen"

// Queries binds the generated queries to a pool, a connection or a
// transaction, so a handler can run them inside the transaction that also
// writes the outbox (STK-4).
func Queries(db sqlcgen.DBTX) *sqlcgen.Queries { return sqlcgen.New(db) }

// The rows of teaching's own tables, named here so a handler can pass one
// around without importing the generated package itself. Every one of these is
// teaching's own truth: it publishes these facts and copies nobody else's.
type (
	// Student is one row of students, a name and a phone number.
	Student = sqlcgen.Student
	// Class is one row of classes, including the rate in force on it now.
	Class = sqlcgen.Class
	// Session is one row of sessions, always concrete rather than computed
	// from a recurrence rule at read time.
	Session = sqlcgen.Session
	// RosterPeriod is one dated membership of a student in a class, with at
	// most one open row per pair.
	RosterPeriod = sqlcgen.RosterPeriod
	// Attendance is one mark for one student in one session.
	Attendance = sqlcgen.Attendance
)
