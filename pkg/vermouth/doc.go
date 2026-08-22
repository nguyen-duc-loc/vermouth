// Package vermouth is the shared module every Vermouth service imports.
//
// Spec 0002, STK-2: every piece of cross service machinery lives here once,
// never copied into a service: the event envelope, the outbox writer, the
// relay, the handled events store, bounded retry and the dead letter park,
// configuration loading, logging, token verification, health and readiness,
// and topic creation.
//
// It carries no independent version (STK-24): it moves in lockstep with the
// services that import it, which is why they share one repository and why a
// service image builds with the repository root as its Docker build context.
package vermouth
