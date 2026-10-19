-- +goose Up
-- Family model: a user with invite_user_id IS NULL is a family head; members
-- point at the head of the family they joined. panel_token unlocks the web
-- panel for the head, invite_code is the payload of the invitation deep link.
ALTER TABLE users
    ADD COLUMN invite_user_id BIGINT REFERENCES users (user_id) ON DELETE SET NULL,
    ADD COLUMN panel_token    TEXT UNIQUE,
    ADD COLUMN invite_code    TEXT UNIQUE;

CREATE INDEX users_invite_user_id_idx ON users (invite_user_id);

-- +goose Down
DROP INDEX users_invite_user_id_idx;

ALTER TABLE users
    DROP COLUMN invite_code,
    DROP COLUMN panel_token,
    DROP COLUMN invite_user_id;
