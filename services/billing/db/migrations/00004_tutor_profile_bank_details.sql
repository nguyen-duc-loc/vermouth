-- The invoice profile gains a stable bank identifier and an optimistic
-- revision. The generated gate remains the one database answer for whether
-- all invoice fields are present.
--
-- +goose Up

ALTER TABLE invoice_profiles
    DROP COLUMN is_complete,
    ADD COLUMN bank_code text,
    ADD COLUMN revision bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT invoice_profiles_revision_nonnegative CHECK (revision >= 0);

ALTER TABLE invoice_profiles
    ADD COLUMN is_complete boolean NOT NULL GENERATED ALWAYS AS (
            NULLIF(btrim(legal_name), '') IS NOT NULL
        AND NULLIF(btrim(contact_line), '') IS NOT NULL
        AND NULLIF(btrim(bank_code), '') IS NOT NULL
        AND NULLIF(btrim(bank_name), '') IS NOT NULL
        AND NULLIF(btrim(bank_account_number), '') IS NOT NULL
        AND NULLIF(btrim(bank_account_holder), '') IS NOT NULL
    ) STORED;

-- +goose Down

ALTER TABLE invoice_profiles
    DROP COLUMN is_complete,
    DROP CONSTRAINT invoice_profiles_revision_nonnegative,
    DROP COLUMN revision,
    DROP COLUMN bank_code;

ALTER TABLE invoice_profiles
    ADD COLUMN is_complete boolean NOT NULL GENERATED ALWAYS AS (
            NULLIF(btrim(legal_name), '') IS NOT NULL
        AND NULLIF(btrim(contact_line), '') IS NOT NULL
        AND NULLIF(btrim(bank_name), '') IS NOT NULL
        AND NULLIF(btrim(bank_account_number), '') IS NOT NULL
        AND NULLIF(btrim(bank_account_holder), '') IS NOT NULL
    ) STORED;
