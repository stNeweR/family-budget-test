-- +goose Up
ALTER TABLE users
    ADD COLUMN invite_user_id BIGINT REFERENCES users (user_id) ON DELETE SET NULL,
    ADD COLUMN invite_code    TEXT UNIQUE;

CREATE INDEX users_invite_user_id_idx ON users (invite_user_id);

-- +goose Down
DROP INDEX users_invite_user_id_idx;

ALTER TABLE users
    DROP COLUMN invite_code,
    DROP COLUMN invite_user_id;
