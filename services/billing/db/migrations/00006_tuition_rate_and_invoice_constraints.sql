-- Projection revisions make same date corrections deterministic. The partial
-- unique indexes make one live run and one direct replacement database facts.
--
-- +goose Up
ALTER TABLE class_rates
    ADD COLUMN rate_revision bigint NOT NULL DEFAULT 0,
    ADD COLUMN source_partition integer,
    ADD COLUMN source_offset bigint,
    ADD CONSTRAINT class_rates_rate_revision_nonnegative CHECK (rate_revision >= 0),
    ADD CONSTRAINT class_rates_rate_bounded CHECK (rate_amount <= 1000000000),
    ADD CONSTRAINT class_rates_source_coordinate_complete CHECK (
        (source_partition IS NULL AND source_offset IS NULL)
        OR (source_partition >= 0 AND source_offset >= 0)
    ),
    ADD CONSTRAINT class_rates_positive_revision_has_source CHECK (
        rate_revision = 0 OR source_partition IS NOT NULL
    );

CREATE UNIQUE INDEX billing_runs_one_live_period_idx
    ON billing_runs (tutor_id, period_year, period_month)
    WHERE superseded_at IS NULL;

CREATE UNIQUE INDEX invoices_one_student_per_run_idx
    ON invoices (tutor_id, billing_run_id, student_id);

CREATE UNIQUE INDEX invoices_one_direct_replacement_idx
    ON invoices (tutor_id, replaces_invoice_id)
    WHERE replaces_invoice_id IS NOT NULL;

-- +goose Down
DROP INDEX invoices_one_direct_replacement_idx;
DROP INDEX invoices_one_student_per_run_idx;
DROP INDEX billing_runs_one_live_period_idx;

ALTER TABLE class_rates
    DROP CONSTRAINT class_rates_positive_revision_has_source,
    DROP CONSTRAINT class_rates_source_coordinate_complete,
    DROP CONSTRAINT class_rates_rate_bounded,
    DROP CONSTRAINT class_rates_rate_revision_nonnegative,
    DROP COLUMN source_offset,
    DROP COLUMN source_partition,
    DROP COLUMN rate_revision;
