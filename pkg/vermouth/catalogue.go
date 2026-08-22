package vermouth

// The topics and event names of spec 0001's catalogue, in one place so one
// name means one value everywhere (spec 0002, conventions this stack fixes).
//
// One topic per publishing service, partitioned by the envelope key, with the
// event name inside the envelope (STK-11). notifications publishes nothing, so
// it has no topic.
//
// Only names live here. The fields of an event are declared by the publisher
// with the full catalogue list, and by each consumer with the fields it
// actually needs, which is what keeps a consumer a tolerant reader (INV-12).
const (
	TopicIdentity = "identity.events"
	TopicTeaching = "teaching.events"
	TopicBilling  = "billing.events"
)

// Published by identity.
const (
	EventTutorRegistered     = "identity.tutor.registered"
	EventTutorProfileChanged = "identity.tutor.profile.changed"
)

// Published by teaching.
const (
	EventClassCreated      = "teaching.class.created"
	EventClassChanged      = "teaching.class.changed"
	EventClassRateChanged  = "teaching.class.rate.changed"
	EventSessionScheduled  = "teaching.session.scheduled"
	EventSessionMoved      = "teaching.session.moved"
	EventSessionCancelled  = "teaching.session.cancelled"
	EventAttendanceMarked  = "teaching.attendance.marked"
	EventStudentRegistered = "teaching.student.registered"
	EventStudentChanged    = "teaching.student.changed"
	EventStudentRemoved    = "teaching.student.removed"
	EventRosterJoined      = "teaching.roster.joined"
	EventRosterLeft        = "teaching.roster.left"
)

// Published by billing.
const (
	EventInvoiceIssued = "billing.invoice.issued"
	EventInvoiceVoided = "billing.invoice.voided"
)
