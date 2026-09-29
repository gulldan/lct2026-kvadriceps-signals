-- +goose Up
CREATE TABLE app_settings_revision (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0)
);
INSERT INTO app_settings_revision DEFAULT VALUES;
CREATE TABLE app_settings_audit (
    revision bigint PRIMARY KEY,
    actor_id uuid NOT NULL,
    changed_keys text[] NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE app_settings_audit;
DROP TABLE app_settings_revision;
