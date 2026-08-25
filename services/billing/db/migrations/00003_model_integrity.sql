-- Authoritative billing references must match the tenant as well as the entity
-- id. Projection tables stay free of foreign keys because their events may
-- arrive in either order, but their queries match tutor_id explicitly.
--
-- +goose Up

-- +goose StatementBegin
DO $$
DECLARE
    mismatches text;
BEGIN
    SELECT string_agg(problem, ', ' ORDER BY problem)
    INTO mismatches
    FROM (
        SELECT format(
            'invoices(invoice_id=%s,tutor_id=%s,billing_run_id=%s,parent_tutor_id=%s)',
            i.invoice_id,
            i.tutor_id,
            i.billing_run_id,
            br.tutor_id
        ) AS problem
        FROM invoices i
        JOIN billing_runs br ON br.billing_run_id = i.billing_run_id
        WHERE i.tutor_id <> br.tutor_id

        UNION ALL

        SELECT format(
            'invoices(invoice_id=%s,tutor_id=%s,replaces_invoice_id=%s,parent_tutor_id=%s)',
            i.invoice_id,
            i.tutor_id,
            i.replaces_invoice_id,
            replaced.tutor_id
        )
        FROM invoices i
        JOIN invoices replaced ON replaced.invoice_id = i.replaces_invoice_id
        WHERE i.tutor_id <> replaced.tutor_id

        UNION ALL

        SELECT format(
            'invoice_lines(invoice_line_id=%s,tutor_id=%s,invoice_id=%s,parent_tutor_id=%s)',
            il.invoice_line_id,
            il.tutor_id,
            il.invoice_id,
            i.tutor_id
        )
        FROM invoice_lines il
        JOIN invoices i ON i.invoice_id = il.invoice_id
        WHERE il.tutor_id <> i.tutor_id
    ) AS found;

    IF mismatches IS NOT NULL THEN
        RAISE EXCEPTION 'billing tenant reference mismatches: %', mismatches;
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE billing_runs
    ADD CONSTRAINT billing_runs_tutor_run_unique UNIQUE (tutor_id, billing_run_id);

ALTER TABLE invoices
    ADD CONSTRAINT invoices_tutor_invoice_unique UNIQUE (tutor_id, invoice_id),
    ADD CONSTRAINT invoices_tutor_billing_run_fk
        FOREIGN KEY (tutor_id, billing_run_id) REFERENCES billing_runs (tutor_id, billing_run_id),
    ADD CONSTRAINT invoices_tutor_replacement_fk
        FOREIGN KEY (tutor_id, replaces_invoice_id) REFERENCES invoices (tutor_id, invoice_id);

ALTER TABLE invoice_lines
    ADD CONSTRAINT invoice_lines_tutor_invoice_fk
        FOREIGN KEY (tutor_id, invoice_id) REFERENCES invoices (tutor_id, invoice_id);

ALTER TABLE class_rates
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE class_rates
    DROP COLUMN updated_at;

ALTER TABLE invoice_lines
    DROP CONSTRAINT invoice_lines_tutor_invoice_fk;

ALTER TABLE invoices
    DROP CONSTRAINT invoices_tutor_replacement_fk,
    DROP CONSTRAINT invoices_tutor_billing_run_fk,
    DROP CONSTRAINT invoices_tutor_invoice_unique;

ALTER TABLE billing_runs
    DROP CONSTRAINT billing_runs_tutor_run_unique;
