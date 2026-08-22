// Package handler holds teaching's work, separate from how it is reached.
//
// Empty until the feature that gives teaching something to do. When it lands, the
// business write and its outbox insert belong in the same pgx.Tx and the same
// function, the way services/identity does it (INV-3, STK-4).
package handler
