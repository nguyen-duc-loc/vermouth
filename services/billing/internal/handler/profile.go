package handler

import "github.com/nguyen-duc-loc/vermouth/services/billing/internal/store"

// The invoice profile fields, in the order the profile screen shows them, so a
// refusal names them in an order the tutor can follow.
const (
	FieldLegalName         = "legal_name"
	FieldContactLine       = "contact_line"
	FieldBankName          = "bank_name"
	FieldBankAccountNumber = "bank_account_number"
	FieldBankAccountHolder = "bank_account_holder"
)

// MissingProfileFields names the fields still to fill in, for the
// profile_incomplete message. It is the message, never the decision: whether the
// month end run may proceed is invoice_profiles.is_complete, the one gate in the
// schema that Postgres recomputes from the same row (AC-12). This function exists
// only so the refusal can say which field to go and fill, and a test asserts an
// empty list here is the same answer as is_complete, so the two cannot drift.
func MissingProfileFields(profile store.InvoiceProfile) []string {
	// The same five fields the generated column tests, in the order the screen
	// shows them. One list rather than five if statements, so this cannot drift
	// from the gate by forgetting one.
	fields := []struct {
		name  string
		value string
	}{
		{FieldLegalName, profile.LegalName.String},
		{FieldContactLine, profile.ContactLine.String},
		{FieldBankName, profile.BankName.String},
		{FieldBankAccountNumber, profile.BankAccountNumber.String},
		{FieldBankAccountHolder, profile.BankAccountHolder.String},
	}

	missing := make([]string, 0, len(fields))
	for _, field := range fields {
		// pgtype.Text leaves String empty when the column is null, so one check
		// covers a null and an empty string alike, which is exactly what the
		// generated column does.
		if field.value == "" {
			missing = append(missing, field.name)
		}
	}
	return missing
}
