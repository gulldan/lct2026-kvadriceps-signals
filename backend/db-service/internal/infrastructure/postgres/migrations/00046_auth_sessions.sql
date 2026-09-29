-- +goose Up
CREATE TABLE auth_sessions (
    id uuid PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_sessions_expiry ON auth_sessions(expires_at);
CREATE INDEX auth_sessions_user ON auth_sessions(user_id);
CREATE TABLE auth_refresh_tokens (
    hash text PRIMARY KEY CHECK (hash ~ '^[0-9a-f]{64}$'),
    session_id uuid NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
    consumed_at timestamptz
);
CREATE INDEX auth_refresh_tokens_session ON auth_refresh_tokens(session_id);

-- +goose Down
DROP TABLE auth_refresh_tokens;
DROP TABLE auth_sessions;
