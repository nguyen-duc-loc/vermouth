-- The recurrence write surface is deployed only after this migration proves
-- existing rows are safe and gives Postgres the final say under concurrency.
--
-- +goose Up

CREATE EXTENSION IF NOT EXISTS btree_gist;

-- +goose StatementBegin
DO $$
DECLARE
    conflicts text;
BEGIN
    SELECT string_agg(
        format('%s:%s<->%s', left_session.tutor_id, left_session.session_id, right_session.session_id),
        ', ' ORDER BY left_session.tutor_id, left_session.starts_at,
                     left_session.session_id, right_session.session_id
    )
    INTO conflicts
    FROM sessions left_session
    JOIN sessions right_session
      ON right_session.tutor_id = left_session.tutor_id
     AND right_session.session_id > left_session.session_id
     AND tstzrange(right_session.starts_at, right_session.ends_at, '[)')
         && tstzrange(left_session.starts_at, left_session.ends_at, '[)')
    WHERE left_session.cancelled_at IS NULL
      AND left_session.superseded_at IS NULL
      AND right_session.cancelled_at IS NULL
      AND right_session.superseded_at IS NULL;

    IF conflicts IS NOT NULL THEN
        RAISE EXCEPTION 'active teaching session overlaps remain: %', conflicts;
    END IF;
END;
$$;
-- +goose StatementEnd

ALTER TABLE sessions
    ADD CONSTRAINT sessions_active_time_no_overlap
    EXCLUDE USING gist (
        tutor_id WITH =,
        tstzrange(starts_at, ends_at, '[)') WITH &&
    )
    WHERE (cancelled_at IS NULL AND superseded_at IS NULL);

ALTER TABLE schedule_rules
    ADD CONSTRAINT schedule_rules_active_dates_no_overlap
    EXCLUDE USING gist (
        tutor_id WITH =,
        class_id WITH =,
        daterange(valid_from, valid_through, '[]') WITH &&
    )
    WHERE (retired_at IS NULL);

-- +goose Down
ALTER TABLE schedule_rules DROP CONSTRAINT schedule_rules_active_dates_no_overlap;
ALTER TABLE sessions DROP CONSTRAINT sessions_active_time_no_overlap;
DROP EXTENSION btree_gist;
