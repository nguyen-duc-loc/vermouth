// Package consumer keeps billing's local copies current from events.
//
// Empty until feature 4 decides the tables. Spec 0001's catalogue already fixes
// what will arrive here: sessions, roster periods, attendance, class labels,
// student labels and dated rate history from teaching, plus an empty invoice
// profile row from identity.tutor.registered. Every one of those is a
// projection, rebuildable by a replay; billing's invoices, numbers, voids and
// paid state are authoritative records and no replay rewrites them.
package consumer
