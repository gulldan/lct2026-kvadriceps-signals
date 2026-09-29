-- +goose Up
-- A locator the contract can express must be storable, otherwise the part of it
-- that cannot be written is silently lost on the way in. element_path carries
-- what an input type offers instead of coordinates -- an HTML element path or a
-- patent claim number -- and snapshot_sha256 pins the exact stored text the
-- offsets address, so a later revision cannot be mistaken for the one the quote
-- was read from.
ALTER TABLE technology_evidence
    ADD COLUMN element_path TEXT NOT NULL DEFAULT ''
        CHECK (length(element_path) <= 512),
    ADD COLUMN snapshot_sha256 TEXT NOT NULL DEFAULT ''
        CHECK (snapshot_sha256 = '' OR snapshot_sha256 ~ '^[0-9a-f]{64}$');

-- +goose Down
ALTER TABLE technology_evidence
    DROP COLUMN element_path,
    DROP COLUMN snapshot_sha256;
