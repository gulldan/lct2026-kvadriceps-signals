-- +goose Up
CREATE TABLE cluster_publications (
    owner_id text NOT NULL,
    id text NOT NULL,
    request_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, id)
);
CREATE TABLE cluster_heads (
    owner_id text PRIMARY KEY,
    publication_id text,
    FOREIGN KEY (owner_id, publication_id) REFERENCES cluster_publications(owner_id, id)
);
ALTER TABLE clusters ADD COLUMN publication_id text;
-- Частичность прежнего набора доказать нельзя; новую голову публикует воркер.
UPDATE clusters SET publication_id = params->>'publish_version' WHERE params ? 'publish_version';
CREATE INDEX clusters_owner_publication ON clusters(owner_id, publication_id);

-- +goose Down
DROP INDEX clusters_owner_publication;
ALTER TABLE clusters DROP COLUMN publication_id;
DROP TABLE cluster_heads;
DROP TABLE cluster_publications;
