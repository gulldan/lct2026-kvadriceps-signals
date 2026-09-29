-- name: GetTrendLiveSearch :one
SELECT query, terms_key, terms, papers, sources, observed_at, expires_at, detail, summary
FROM trend_live_searches
WHERE query = $1;

-- name: GetFreshTrendLiveSearchByTerms :one
-- Близкий запрос: тот же набор терминов после перевода, ещё не протухший.
SELECT query, terms_key, terms, papers, sources, observed_at, expires_at, detail, summary
FROM trend_live_searches
WHERE terms_key = $1 AND expires_at > now()
ORDER BY observed_at DESC
LIMIT 1;

-- name: UpsertTrendLiveSearch :exec
INSERT INTO trend_live_searches (query, terms_key, terms, papers, sources, observed_at, expires_at, detail, summary)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (query) DO UPDATE SET
  terms_key   = excluded.terms_key,
  terms       = excluded.terms,
  papers      = excluded.papers,
  sources     = excluded.sources,
  observed_at = excluded.observed_at,
  expires_at  = excluded.expires_at,
  detail      = excluded.detail,
  summary     = excluded.summary;

-- name: DeleteTrendLiveRecords :exec
DELETE FROM trend_live_records WHERE query = $1;

-- name: InsertTrendLiveRecords :exec
-- Записи приходят одним JSONB и разворачиваются в строки на стороне базы:
-- один запрос вместо сотен вставок по записи.
INSERT INTO trend_live_records (
  query, url, channel, source, title, published_on,
  language, publisher_type, trust, trust_basis, content_hash, observed_at)
-- Отсутствующее поле записи — пустая строка, а не NULL: столбцы объявлены
-- NOT NULL, и запись без языка или хеша роняла сохранение всего поиска.
SELECT sqlc.arg(query), r.url, coalesce(r.channel, ''), coalesce(r.source, ''),
       coalesce(r.title, ''), coalesce(r.published_on, ''), coalesce(r.language, ''),
       coalesce(r.publisher_type, ''), coalesce(r.trust, ''), coalesce(r.trust_basis, ''),
       coalesce(r.content_hash, ''), sqlc.arg(observed_at)
FROM jsonb_to_recordset(sqlc.arg(records)::jsonb) AS r(
  url TEXT, channel TEXT, source TEXT, title TEXT, published_on TEXT,
  language TEXT, publisher_type TEXT, trust TEXT, trust_basis TEXT, content_hash TEXT)
WHERE r.url <> ''
ON CONFLICT (query, url) DO NOTHING;

-- name: CountTrendLiveRecords :one
SELECT count(*) FROM trend_live_records WHERE query = $1;

-- name: DeleteSupersededTrendLiveSearches :exec
-- Ключ поиска начинается с версии формата выдачи. Пересобранный по новой
-- версии запрос делает прежнюю строку того же текста мусором: открыть её
-- уже нельзя, а ночная пересборка брала бы её снова и снова. Протухшие по
-- сроку поиски не удаляются вовсе: собранное по запросу оператора хранится,
-- пока его не заменит новый сбор по тому же запросу.
DELETE FROM trend_live_searches AS old
USING trend_live_searches AS new
WHERE old.query <> new.query
  AND substr(old.query, position('|' IN old.query) + 1) = substr(new.query, position('|' IN new.query) + 1)
  AND old.observed_at < new.observed_at;

-- name: ListTrendLiveSearches :many
-- История запросов: свежие первыми. Ответ не отдаётся, только его паспорт и
-- короткое описание направления для карты.
SELECT query, terms, papers, sources, observed_at, expires_at, summary
FROM trend_live_searches
ORDER BY observed_at DESC
LIMIT sqlc.arg(row_limit);
