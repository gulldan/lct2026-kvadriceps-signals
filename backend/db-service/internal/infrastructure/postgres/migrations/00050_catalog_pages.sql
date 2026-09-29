-- +goose Up
CREATE INDEX documents_owner_created_id ON documents(owner_id,created_at DESC,id DESC);
CREATE INDEX documents_created_id ON documents(created_at DESC,id DESC);
CREATE INDEX hypotheses_owner_created_id ON hypotheses(owner_id,created_at DESC,id DESC);
-- +goose StatementBegin
CREATE FUNCTION document_file_type(filename text, mime text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN CASE
 WHEN lower(filename) ~ '\.pdf$' THEN 'pdf'
 WHEN lower(filename) ~ '\.docx?$' THEN 'docx'
 WHEN lower(filename) ~ '\.(eml|msg)$' THEN 'eml'
 WHEN lower(filename) ~ '\.xlsx?$' THEN 'xlsx'
 WHEN lower(filename) ~ '\.pptx?$' THEN 'pptx'
 WHEN lower(filename) ~ '\.(txt|md)$' THEN 'txt'
 WHEN lower(filename) ~ '\.(png|jpg|jpeg|webp|tiff)$' THEN 'image'
 WHEN lower(filename) ~ '\.(db|sqlite|sqlite3)$' THEN 'db'
 WHEN lower(mime) LIKE '%pdf%' THEN 'pdf'
 WHEN lower(mime) LIKE 'image/%' THEN 'image'
 WHEN lower(mime) ~ '(wordprocessingml|msword)' THEN 'docx'
 WHEN lower(mime) ~ '(spreadsheetml|excel)' THEN 'xlsx'
 WHEN lower(mime) ~ '(presentationml|powerpoint)' THEN 'pptx'
 WHEN lower(mime) = 'message/rfc822' THEN 'eml'
 WHEN lower(mime) LIKE '%sqlite%' THEN 'db'
 WHEN lower(mime) LIKE 'text/%' THEN 'txt'
 ELSE 'other' END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION hypothesis_queue(status text, assessment jsonb) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN CASE
 WHEN status IN ('rejected','archived') THEN ''
 WHEN status='approved' OR assessment#>>'{check,verdict}'='supported' THEN 'ready'
 WHEN assessment#>>'{check,verdict}' IN ('refuted','mixed') THEN 'risk'
 WHEN assessment#>>'{check,verdict}'='insufficient' THEN 'insufficient'
 ELSE 'needs_verify' END;
-- +goose StatementEnd
-- +goose Down
DROP FUNCTION hypothesis_queue(text,jsonb);
DROP FUNCTION document_file_type(text,text);
DROP INDEX hypotheses_owner_created_id;
DROP INDEX documents_created_id;
DROP INDEX documents_owner_created_id;
