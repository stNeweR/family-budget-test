-- +goose Up
CREATE TABLE users (
    user_id   BIGINT PRIMARY KEY,
    username  TEXT NOT NULL DEFAULT '',
    full_name TEXT NOT NULL DEFAULT ''
);

CREATE TABLE expenses (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    BIGINT NOT NULL REFERENCES users (user_id) ON DELETE CASCADE,
    amount     NUMERIC(12, 2) NOT NULL CHECK (amount > 0),
    category   TEXT NOT NULL CHECK (
        category IN ('еда', 'транспорт', 'жильё', 'развлечения', 'здоровье', 'покупки', 'другое')
    ),
    spent_at   DATE NOT NULL,
    comment    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX expenses_user_spent_at_idx ON expenses (user_id, spent_at DESC);
CREATE INDEX expenses_spent_at_idx ON expenses (spent_at DESC);
CREATE INDEX expenses_category_idx ON expenses (category);

-- +goose Down
DROP TABLE expenses;
DROP TABLE users;
