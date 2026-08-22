// Package store reaches notifications' own database and nothing else (STK-5,
// INV-2).
package store

import "github.com/nguyen-duc-loc/vermouth/services/notifications/internal/store/sqlcgen"

// Queries binds the generated queries to a pool, a connection or a
// transaction, so a consumer can write its projection inside the transaction
// that also records the event as handled (INV-5).
func Queries(db sqlcgen.DBTX) *sqlcgen.Queries { return sqlcgen.New(db) }
