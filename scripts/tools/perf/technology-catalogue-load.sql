-- OPS-07: синтетический корпус целевого объёма. Тексты сгенерированы, но их
-- длины и словарь взяты близкими к реальным: замер латентности зависит от
-- размера строк и селективности LIKE, а не от осмысленности содержания.
--
-- Объём задаётся переменными psql, по умолчанию целевой:
--   psql -v techs=3000 -v mentions=30000 -f technology-catalogue-load.sql
-- Упоминаний должно быть не меньше, чем технологий: технология без упоминания
-- не проходит отложенный триггер и корпус не загрузится вовсе.
\set ON_ERROR_STOP on
\timing on
\if :{?docs}
\else
\set docs 2000
\endif
\if :{?techs}
\else
\set techs 100000
\endif
\if :{?mentions}
\else
\set mentions 1000000
\endif

-- Технологии не связаны с документами внешним ключом, поэтому повторный прогон
-- на другом объёме должен убрать их явно — иначе он упадёт на дубликате ключа.
DELETE FROM technologies WHERE id LIKE 'ops07-%';
DELETE FROM documents WHERE id LIKE 'ops07-%';

-- Триггер technologies_require_mention отложенный: он проверяется на COMMIT,
-- поэтому загрузка идёт одной транзакцией — технология без упоминания не
-- существует даже мгновение.
BEGIN;

-- Документы-носители: mentions ссылаются на них, и через них работает правило
-- доступа. Реальный стенд держит около 15 000; меньшее число здесь делает
-- разрешённый набор плотнее, то есть тест не мягче реальности.
INSERT INTO documents (id, owner_id, object_key, filename, status, source_metadata)
SELECT
    'ops07-doc-' || i,
    '00000000-0000-0000-0000-000000000001',
    'ops07/' || i,
    'ops07-' || i || '.pdf',
    'indexed',
    jsonb_build_object(
        'version', '1',
        'source_kind', 'paper',
        'provider', 'arxiv',
        'access_scope', 'public',
        'native_id', 'ops07-' || i,
        'canonical_id', 'ops07-' || i,
        'revision', 'v1',
        'content_hash', md5('ops07-' || i) || md5('ops07-rev-' || i),
        'hash_algorithm', 'sha256',
        'observed_at', '2026-09-13T00:00:00Z',
        'language', CASE WHEN i % 2 = 0 THEN 'ru' ELSE 'en' END)
FROM generate_series(1, :docs) AS i;

-- Технологии.
INSERT INTO technologies (id, canonical_name, name_kind, description, validation_status, method_version)
SELECT
    'ops07-tech-' || i,
    (ARRAY['адаптивная','каскадная','гибридная','разреженная','контекстная','итеративная',
           'спекулятивная','иерархическая'])[1 + i % 8] || ' ' ||
    (ARRAY['маршрутизация','сепарация','декодировка','агрегация','флотация','кластеризация',
           'выщелачивание','сегментация'])[1 + (i / 8) % 8] || ' ' || i,
    CASE WHEN i % 3 = 0 THEN 'descriptive' ELSE 'authored' END,
    'Метод решает задачу ' || (ARRAY['обогащения руды','выбора способа поиска','снижения энергозатрат',
        'повышения содержания железа','ускорения вывода модели'])[1 + i % 5] ||
    ' за счёт механизма, описанного в разделе источника. Условия и ограничения указаны отдельно; ' ||
    'величина преимущества приводится с базой сравнения. Запись ' || i || '.',
    (ARRAY['proposed','verified','needs_review'])[1 + i % 3],
    'ops07-v1'
FROM generate_series(1, :techs) AS i;

-- Синонимы: примерно у половины технологий, как в размеченном наборе.
INSERT INTO technology_aliases (technology_id, value, language, basis)
SELECT 'ops07-tech-' || i,
       'alias ' || (ARRAY['routing','separation','decoding','aggregation','flotation'])[1 + i % 5] || ' ' || i,
       CASE WHEN i % 2 = 0 THEN 'ru' ELSE 'en' END,
       'source_text'
FROM generate_series(1, :techs, 2) AS i;

-- Упоминание ссылается на прогон извлечения по своей ревизии: составной FK
-- требует записи в technology_extractions на каждый документ.
INSERT INTO technology_extractions (source_revision_id, extractor_version, schema_version,
                                    status, processed_sections, total_sections)
SELECT 'ops07-doc-' || i, 'ops07-extractor-v1', 'technology-extraction-v1', 'complete', 12, 12
FROM generate_series(1, :docs) AS i;

-- Упоминания: по десять на технологию, разложенных по документам.
INSERT INTO technology_mentions (id, technology_id, source_revision_id, extractor_version, schema_version,
                                 name, description, role, problem, mechanism, inputs, outputs, extraction_status)
SELECT
    'ops07-m-' || g,
    'ops07-tech-' || (1 + g % :techs),
    'ops07-doc-' || (1 + g % :docs),
    'ops07-extractor-v1',
    'technology-extraction-v1',
    'упоминание ' || g,
    'Упоминание описывает применение метода в конкретной ревизии источника. Номер ' || g || '.',
    (ARRAY['proposed_contribution','evaluated_application','claimed_invention','background'])[1 + g % 4],
    'задача ' || (ARRAY['обогащение','маршрутизация','декодирование','сепарация'])[1 + g % 4] || ' ' || g,
    'механизм ' || (ARRAY['магнитный','гравитационный','флотационный','обжиговый'])[1 + g % 4] || ' ' || g,
    ARRAY['вход ' || g],
    ARRAY['выход ' || (ARRAY['концентрат','ранжирование','сегменты','хвосты'])[1 + g % 4]],
    (ARRAY['complete','partial','needs_review'])[1 + g % 3]
FROM generate_series(1, :mentions) AS g;

COMMIT;

ANALYZE documents;
ANALYZE technologies;
ANALYZE technology_aliases;
ANALYZE technology_mentions;
