-- +goose Up
-- A cursor must be invalidated by mention/alias/ACL changes too. The previous
-- count/max(updated_at) of technologies missed them and scanned every search.
CREATE TABLE technology_catalogue_epoch (
    id BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    value BIGINT NOT NULL
);
INSERT INTO technology_catalogue_epoch VALUES (true, 1);

-- +goose StatementBegin
CREATE FUNCTION advance_technology_catalogue_epoch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE technology_catalogue_epoch SET value=value+1 WHERE id=true;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER technologies_catalogue_epoch AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON technologies
    FOR EACH STATEMENT EXECUTE FUNCTION advance_technology_catalogue_epoch();
CREATE TRIGGER aliases_catalogue_epoch AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON technology_aliases
    FOR EACH STATEMENT EXECUTE FUNCTION advance_technology_catalogue_epoch();
CREATE TRIGGER mentions_catalogue_epoch AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON technology_mentions
    FOR EACH STATEMENT EXECUTE FUNCTION advance_technology_catalogue_epoch();
CREATE TRIGGER documents_catalogue_epoch AFTER UPDATE OF source_metadata, owner_id ON documents
    FOR EACH STATEMENT EXECUTE FUNCTION advance_technology_catalogue_epoch();
CREATE TRIGGER access_catalogue_epoch AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON source_index_heads
    FOR EACH STATEMENT EXECUTE FUNCTION advance_technology_catalogue_epoch();

-- +goose Down
DROP TRIGGER access_catalogue_epoch ON source_index_heads;
DROP TRIGGER documents_catalogue_epoch ON documents;
DROP TRIGGER mentions_catalogue_epoch ON technology_mentions;
DROP TRIGGER aliases_catalogue_epoch ON technology_aliases;
DROP TRIGGER technologies_catalogue_epoch ON technologies;
DROP FUNCTION advance_technology_catalogue_epoch();
DROP TABLE technology_catalogue_epoch;
