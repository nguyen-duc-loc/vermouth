// Package model holds the guards on the data model that no single service can
// hold: the ones that read every service's SQL, or every service's live schema,
// and assert the one model spec 0003 fixed.
//
// It carries only tests. A guard that belongs to one service lives in that
// service's own module, beside the tables it guards; what lands here is what
// only makes sense across all four, which is also why this module reaches four
// databases and no service ever does (STK-5).
package model
