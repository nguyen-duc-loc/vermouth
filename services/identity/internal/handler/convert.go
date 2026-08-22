package handler

import (
	"time"

	"github.com/google/uuid"

	"github.com/nguyen-duc-loc/vermouth/services/identity/internal/store/sqlcgen"
)

// toRegisteredFields takes the row a first sign in inserted to the catalogue's
// field list for identity.tutor.registered. It is a conversion rather than a
// struct literal at the call site because the two shapes are deliberately
// separate: a column added to tutors must never reach an event by accident.
func toRegisteredFields(row sqlcgen.InsertTutorRow) tutorFields {
	return tutorFields{
		TutorID:     row.TutorID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Timezone:    row.Timezone,
		Language:    row.Language,
	}
}

// toChangedFields does the same for identity.tutor.profile.changed, which spec
// 0001 gives the same five fields. updated_at is the row's own stamp and is not
// among them.
func toChangedFields(row sqlcgen.UpdateTutorFromProviderRow) tutorFields {
	return tutorFields{
		TutorID:     row.TutorID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Timezone:    row.Timezone,
		Language:    row.Language,
	}
}

func toTutor(tutorID uuid.UUID, email, displayName, timezone, language string, createdAt time.Time) Tutor {
	return Tutor{
		TutorID:     tutorID,
		Email:       email,
		DisplayName: displayName,
		Timezone:    timezone,
		Language:    language,
		CreatedAt:   createdAt,
	}
}
