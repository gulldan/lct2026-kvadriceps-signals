-- +goose Up
-- Шестнадцать якорных регулярных выражений на строку стоили 140 мс на 29 тыс. документов в сводке
-- каталога: расширение берётся одним split_part, а разбор mime остаётся запасным путём для строк
-- без точки в имени. Ответ функции не меняется — сверено на всём корпусе стенда.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION document_file_type(filename text, mime text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN CASE
 CASE WHEN strpos(filename,'.')=0 THEN '' ELSE split_part(lower(filename),'.',-1) END
 WHEN 'pdf' THEN 'pdf'
 WHEN 'doc' THEN 'docx' WHEN 'docx' THEN 'docx'
 WHEN 'eml' THEN 'eml' WHEN 'msg' THEN 'eml'
 WHEN 'xls' THEN 'xlsx' WHEN 'xlsx' THEN 'xlsx'
 WHEN 'ppt' THEN 'pptx' WHEN 'pptx' THEN 'pptx'
 WHEN 'txt' THEN 'txt' WHEN 'md' THEN 'txt'
 WHEN 'png' THEN 'image' WHEN 'jpg' THEN 'image' WHEN 'jpeg' THEN 'image'
 WHEN 'webp' THEN 'image' WHEN 'tiff' THEN 'image'
 WHEN 'db' THEN 'db' WHEN 'sqlite' THEN 'db' WHEN 'sqlite3' THEN 'db'
 ELSE CASE
 WHEN lower(mime) LIKE '%pdf%' THEN 'pdf'
 WHEN lower(mime) LIKE 'image/%' THEN 'image'
 WHEN lower(mime) ~ '(wordprocessingml|msword)' THEN 'docx'
 WHEN lower(mime) ~ '(spreadsheetml|excel)' THEN 'xlsx'
 WHEN lower(mime) ~ '(presentationml|powerpoint)' THEN 'pptx'
 WHEN lower(mime) = 'message/rfc822' THEN 'eml'
 WHEN lower(mime) LIKE '%sqlite%' THEN 'db'
 WHEN lower(mime) LIKE 'text/%' THEN 'txt'
 ELSE 'other' END END;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION document_file_type(filename text, mime text) RETURNS text
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
