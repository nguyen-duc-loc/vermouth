package handler

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/unicode/norm"

	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"
	"github.com/nguyen-duc-loc/vermouth/services/billing/internal/store/sqlcgen"
)

const (
	// FieldLegalName is the stable validation and missing field code for invoice identity.
	FieldLegalName = "legal_name"
	// FieldContactLine is the stable validation and missing field code for contact details.
	FieldContactLine = "contact_line"
	// FieldBankCode is the stable validation and missing field code for the selected bank.
	FieldBankCode = "bank_code"
	// FieldBankAccountNumber is the stable validation and missing field code for the account number.
	FieldBankAccountNumber = "bank_account_number"
	// FieldBankAccountHolder is the stable validation and missing field code for the account holder.
	FieldBankAccountHolder = "bank_account_holder"

	profileBankMissing  = "missing"
	profileBankActive   = "active"
	profileBankInactive = "inactive"
	profileFieldCount   = 5
	profileNameMax      = 120
	profileContactMax   = 200
)

var accountNumberPattern = regexp.MustCompile(`^[A-Za-z0-9]{3,34}$`)

// ProfileInput is the complete editable representation accepted from one tutor.
type ProfileInput struct {
	ExpectedRevision  int64
	LegalName         *string
	ContactLine       *string
	BankCode          *string
	BankAccountNumber *string
	BankAccountHolder *string
}

// InvoiceProfile is the canonical private profile response returned to its owner.
type InvoiceProfile struct {
	LegalName         *string  `json:"legal_name"`
	ContactLine       *string  `json:"contact_line"`
	BankCode          *string  `json:"bank_code"`
	BankName          *string  `json:"bank_name"`
	BankAccountNumber *string  `json:"bank_account_number"`
	BankAccountHolder *string  `json:"bank_account_holder"`
	Revision          int64    `json:"revision"`
	IsComplete        bool     `json:"is_complete"`
	MissingFields     []string `json:"missing_fields"`
	BankStatus        string   `json:"bank_status"`
}

// ProfileFieldError is one safe field code and reason returned for correction.
type ProfileFieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ProfileValidationError groups every field problem found in one save.
type ProfileValidationError struct {
	Fields []ProfileFieldError
}

// Error implements error without exposing private profile values.
func (*ProfileValidationError) Error() string { return "invoice profile validation failed" }

// ProfileConflictError reports only the revision needed for safe conflict recovery.
type ProfileConflictError struct {
	CurrentRevision int64
}

// Error implements error without exposing private profile values.
func (*ProfileConflictError) Error() string { return "invoice profile changed after it was read" }

// ProfileService owns profile reads and revision guarded saves.
type ProfileService struct {
	pool *pgxpool.Pool
}

// NewProfileService binds invoice profile work to billing's own database.
func NewProfileService(pool *pgxpool.Pool) *ProfileService { return &ProfileService{pool: pool} }

// Read seeds a missing row idempotently and returns its canonical private representation.
func (s *ProfileService) Read(ctx context.Context, tutorID uuid.UUID) (InvoiceProfile, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("begin profile read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := store.Queries(tx)
	err = queries.SeedInvoiceProfile(ctx, tutorID)
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("seed invoice profile: %w", err)
	}
	profile, err := queries.GetInvoiceProfile(ctx, tutorID)
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("read invoice profile: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("commit profile read: %w", err)
	}
	return profileFromStored(profile), nil
}

// Save rejects malformed values, recognizes identical retries, then commits one change.
func (s *ProfileService) Save(
	ctx context.Context,
	tutorID uuid.UUID,
	input ProfileInput,
) (InvoiceProfile, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("begin profile save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := store.Queries(tx)
	err = queries.SeedInvoiceProfile(ctx, tutorID)
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("seed invoice profile: %w", err)
	}
	current, err := queries.LockInvoiceProfile(ctx, tutorID)
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("lock invoice profile: %w", err)
	}
	normalized, fieldErrors := normalizeProfileInput(input)
	if len(fieldErrors) == 0 && profileInputMatches(current, normalized) {
		err = tx.Commit(ctx)
		if err != nil {
			return InvoiceProfile{}, fmt.Errorf("commit identical profile save: %w", err)
		}
		return profileFromStored(current), nil
	}
	if input.ExpectedRevision != current.Revision {
		return InvoiceProfile{}, &ProfileConflictError{CurrentRevision: current.Revision}
	}

	bankName := retainedBankName(current, normalized.BankCode)
	if normalized.BankCode != nil {
		bank, found := BankByCode(*normalized.BankCode)
		if !found || !bank.Active {
			fieldErrors = append(fieldErrors, ProfileFieldError{Field: FieldBankCode, Reason: "inactive_bank"})
		} else if !sameOptionalText(normalized.BankCode, textPointer(current.BankCode)) {
			bankName = &bank.ShortName
		}
	}
	if len(fieldErrors) > 0 {
		return InvoiceProfile{}, &ProfileValidationError{Fields: fieldErrors}
	}

	updated, err := queries.UpdateInvoiceProfile(ctx, sqlcgen.UpdateInvoiceProfileParams{
		LegalName:         nullableText(normalized.LegalName),
		ContactLine:       nullableText(normalized.ContactLine),
		BankCode:          nullableText(normalized.BankCode),
		BankName:          nullableText(bankName),
		BankAccountNumber: nullableText(normalized.BankAccountNumber),
		BankAccountHolder: nullableText(normalized.BankAccountHolder),
		OwnerTutorID:      tutorID,
	})
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("update invoice profile: %w", err)
	}
	err = tx.Commit(ctx)
	if err != nil {
		return InvoiceProfile{}, fmt.Errorf("commit profile save: %w", err)
	}
	return profileFromStored(updated), nil
}

// MissingProfileFields names absent editable fields in their fixed screen order.
func MissingProfileFields(profile store.InvoiceProfile) []string {
	fields := make([]string, 0, profileFieldCount)
	if strings.TrimSpace(profile.LegalName.String) == "" {
		fields = append(fields, FieldLegalName)
	}
	if strings.TrimSpace(profile.ContactLine.String) == "" {
		fields = append(fields, FieldContactLine)
	}
	if strings.TrimSpace(profile.BankCode.String) == "" || strings.TrimSpace(profile.BankName.String) == "" {
		fields = append(fields, FieldBankCode)
	}
	if strings.TrimSpace(profile.BankAccountNumber.String) == "" {
		fields = append(fields, FieldBankAccountNumber)
	}
	if strings.TrimSpace(profile.BankAccountHolder.String) == "" {
		fields = append(fields, FieldBankAccountHolder)
	}
	return fields
}

func profileFromStored(profile store.InvoiceProfile) InvoiceProfile {
	bankStatus := profileBankMissing
	if profile.BankCode.Valid && strings.TrimSpace(profile.BankCode.String) != "" {
		bankStatus = profileBankInactive
		if bank, found := BankByCode(profile.BankCode.String); found && bank.Active {
			bankStatus = profileBankActive
		}
	}
	return InvoiceProfile{
		LegalName:         textPointer(profile.LegalName),
		ContactLine:       textPointer(profile.ContactLine),
		BankCode:          textPointer(profile.BankCode),
		BankName:          textPointer(profile.BankName),
		BankAccountNumber: textPointer(profile.BankAccountNumber),
		BankAccountHolder: textPointer(profile.BankAccountHolder),
		Revision:          profile.Revision,
		IsComplete:        profile.IsComplete,
		MissingFields:     MissingProfileFields(profile),
		BankStatus:        bankStatus,
	}
}

func normalizeProfileInput(input ProfileInput) (ProfileInput, []ProfileFieldError) {
	normalized := ProfileInput{ExpectedRevision: input.ExpectedRevision}
	fields := make([]ProfileFieldError, 0, profileFieldCount)
	normalized.LegalName = normalizeProfileText(input.LegalName, FieldLegalName, profileNameMax, &fields)
	normalized.ContactLine = normalizeProfileText(input.ContactLine, FieldContactLine, profileContactMax, &fields)
	normalized.BankCode = normalizeOuterSpace(input.BankCode)
	normalized.BankAccountNumber = normalizeAccountNumber(input.BankAccountNumber, &fields)
	normalized.BankAccountHolder = normalizeProfileText(
		input.BankAccountHolder, FieldBankAccountHolder, profileNameMax, &fields,
	)
	return normalized, fields
}

func normalizeProfileText(
	value *string,
	field string,
	maxRunes int,
	errors *[]ProfileFieldError,
) *string {
	if value == nil {
		return nil
	}
	invalid := false
	for _, character := range *value {
		if unicode.IsControl(character) {
			invalid = true
			break
		}
	}
	normalized := strings.Join(strings.Fields(norm.NFC.String(*value)), " ")
	if normalized == "" {
		return nil
	}
	if invalid {
		*errors = append(*errors, ProfileFieldError{Field: field, Reason: "invalid_format"})
	}
	if utf8.RuneCountInString(normalized) > maxRunes {
		*errors = append(*errors, ProfileFieldError{Field: field, Reason: "too_long"})
	}
	return &normalized
}

func normalizeAccountNumber(value *string, errors *[]ProfileFieldError) *string {
	normalized := normalizeOuterSpace(value)
	if normalized == nil {
		return nil
	}
	if !accountNumberPattern.MatchString(*normalized) {
		*errors = append(*errors, ProfileFieldError{Field: FieldBankAccountNumber, Reason: "invalid_format"})
	}
	upper := strings.ToUpper(*normalized)
	return &upper
}

func normalizeOuterSpace(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func profileInputMatches(profile store.InvoiceProfile, input ProfileInput) bool {
	return sameOptionalText(input.LegalName, textPointer(profile.LegalName)) &&
		sameOptionalText(input.ContactLine, textPointer(profile.ContactLine)) &&
		sameOptionalText(input.BankCode, textPointer(profile.BankCode)) &&
		sameOptionalText(input.BankAccountNumber, textPointer(profile.BankAccountNumber)) &&
		sameOptionalText(input.BankAccountHolder, textPointer(profile.BankAccountHolder))
}

func retainedBankName(profile store.InvoiceProfile, nextCode *string) *string {
	currentCode := textPointer(profile.BankCode)
	if sameOptionalText(currentCode, nextCode) {
		return textPointer(profile.BankName)
	}
	return nil
}

func sameOptionalText(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func nullableText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	text := value.String
	return &text
}
