package vermouth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// KeyKind is the field an event is keyed by. Spec 0001's catalogue names one
// per event and five in total; STK-11 lets all five share three topics,
// because INV-7 keeps consumers from depending on order across two keys.
type KeyKind string

const (
	// KeyTutorID keys everything that belongs to one tutor.
	KeyTutorID KeyKind = "tutor_id"
	// KeyClassID keys a class and the sessions generated from it.
	KeyClassID KeyKind = "class_id"
	// KeySessionID keys one session and its attendance.
	KeySessionID KeyKind = "session_id"
	// KeyStudentID keys one student across the classes they sit in.
	KeyStudentID KeyKind = "student_id"
	// KeyInvoiceID keys one invoice and everything said about it later.
	KeyInvoiceID KeyKind = "invoice_id"
)

// The envelope refuses an event it cannot key or name, and says so with a
// static error so a caller can match on the reason rather than on a string.
var (
	errEventNameRequired = errors.New("envelope: event_name is required")
	errEventVersionStart = errors.New("envelope: event_version starts at 1")
	errKeyRequired       = errors.New("envelope: needs a key, which decides its partition (INV-6)")
	errTutorIDRequired   = errors.New("envelope: tutor_id is required for a tenant scoped event")
)

// Key is the partition key: ordering is guaranteed per key and never across
// keys (INV-6). It is a required typed argument to NewEnvelope (STK-19), so
// the key is chosen at the call site next to the business write.
type Key struct {
	Kind  KeyKind   `json:"kind"`
	Value uuid.UUID `json:"value"`
}

// Bytes is what the broker partitions on. Two events about the same class_id
// therefore land on the same partition in publication order.
func (k Key) Bytes() []byte { return []byte(k.Value.String()) }

// Envelope is the shape of every event (INV-4). The catalogue's named fields
// travel in Data, so one decode path serves every event.
type Envelope struct {
	EventID      uuid.UUID       `json:"event_id"`
	EventName    string          `json:"event_name"`
	EventVersion int             `json:"event_version"`
	OccurredAt   time.Time       `json:"occurred_at"`
	TutorID      uuid.UUID       `json:"tutor_id"`
	Key          Key             `json:"key"`
	RequestID    string          `json:"request_id"`
	Data         json.RawMessage `json:"data"`
}

// TutorIDConflictError identifies an event whose payload tries to name a
// different tenant from its trusted envelope. Consumers can reject the event
// before any projection write instead of silently choosing one value.
type TutorIDConflictError struct {
	EventName       string
	EnvelopeTutorID uuid.UUID
	PayloadTutorID  uuid.UUID
}

func (e *TutorIDConflictError) Error() string {
	return fmt.Sprintf(
		"event %s payload tutor_id %s conflicts with envelope tutor_id %s",
		e.EventName,
		e.PayloadTutorID,
		e.EnvelopeTutorID,
	)
}

// TrustedTutorID returns the tenant identity from the envelope. A payload may
// repeat that value for catalogue compatibility, but it can never replace it.
func TrustedTutorID(env Envelope, payloadTutorID uuid.UUID) (uuid.UUID, error) {
	if env.TutorID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%s: %w", env.EventName, errTutorIDRequired)
	}
	if payloadTutorID != uuid.Nil && payloadTutorID != env.TutorID {
		return uuid.Nil, &TutorIDConflictError{
			EventName:       env.EventName,
			EnvelopeTutorID: env.TutorID,
			PayloadTutorID:  payloadTutorID,
		}
	}
	return env.TutorID, nil
}

// NewEnvelope builds an event. Every event in the catalogue starts at
// event_version 1 and the number rises only when a tolerant reader cannot
// absorb the change (INV-4). request_id is taken from the context, so it is
// the one the gateway created (INV-15).
func NewEnvelope(ctx context.Context, eventName string, eventVersion int, tutorID uuid.UUID, key Key, data any) (Envelope, error) {
	if eventName == "" {
		return Envelope{}, errEventNameRequired
	}
	if eventVersion < 1 {
		return Envelope{}, errEventVersionStart
	}
	if key.Kind == "" || key.Value == uuid.Nil {
		return Envelope{}, fmt.Errorf("envelope: %s: %w", eventName, errKeyRequired)
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return Envelope{}, fmt.Errorf("envelope: new event id: %w", err)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, fmt.Errorf("envelope: encode %s fields: %w", eventName, err)
	}
	return Envelope{
		EventID:      eventID,
		EventName:    eventName,
		EventVersion: eventVersion,
		OccurredAt:   time.Now().UTC(),
		TutorID:      tutorID,
		Key:          key,
		RequestID:    RequestID(ctx),
		Data:         encoded,
	}, nil
}

// UnknownVersionError is the one thing a tolerant reader must not absorb: a
// version it does not recognise is parked rather than guessed at (INV-12).
type UnknownVersionError struct {
	EventName string
	Got       int
	Accepted  []int
}

func (e *UnknownVersionError) Error() string {
	return fmt.Sprintf("event %s arrived at version %d, this consumer accepts %v", e.EventName, e.Got, e.Accepted)
}

// DecodeInto checks the accepted version set before it decodes any field
// (STK-20), then decodes the catalogue fields into target. Unknown fields are
// ignored, which is what makes the reader tolerant (INV-12).
func DecodeInto(env Envelope, accepted []int, target any) error {
	if !slices.Contains(accepted, env.EventVersion) {
		return &UnknownVersionError{EventName: env.EventName, Got: env.EventVersion, Accepted: accepted}
	}
	err := json.Unmarshal(env.Data, target)
	if err != nil {
		return fmt.Errorf("decode %s fields: %w", env.EventName, err)
	}
	return nil
}
