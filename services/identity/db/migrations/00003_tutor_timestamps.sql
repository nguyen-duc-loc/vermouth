-- Completes the tutor row spec 0003's ownership table describes, which is now
-- timestamps only: spec 0004 removed the password_hash column this migration
-- used to add, because Vermouth stores no password at all.
--
-- updated_at is what identity.tutor.profile.changed stamps, so the two
-- consumers of that event (billing's profile seed, notifications' recipient)
-- can be replayed in any order and still land on the same row. The event
-- carries only spec 0001's five fields, never this column.
--
-- +goose Up
ALTER TABLE tutors
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE tutors
    DROP COLUMN updated_at;
