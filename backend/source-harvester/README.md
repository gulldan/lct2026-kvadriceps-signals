# source-harvester

Кладёт статьи, репозитории, патенты и страницы сайтов в конвейер документов — через ту же
очередь, что и загрузка пользователя. Один процесс, пять адаптеров: `crossref`, `arxiv`,
`github`, `patent`, `web`. Последние два читают внутренние сборщики из
[backend/trend-sources](../trend-sources/README.md). Место в системе — в
[docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md), профиль Compose — `signalgraph`.

## Пробный сбор

Без `--ingest` страницы пишутся в новый локальный каталог: сырой ответ и его SHA-256, конверты
`sourcedoc` v1, причины отклонений, следующий курсор. Рабочие документы и индексы не трогаются.
Пределы: `--limit` 1..200 записей, `--timeout` до 15 минут (по умолчанию 5), `--page-size`
1..100.

```sh
go run ./cmd/source-harvester \
  --provider crossref --query 'retrieval augmented generation' \
  --from 2023-01-01 --through 2026-08-31 --limit 6 --page-size 2 \
  --output /tmp/tr-crossref-new-run

go run ./cmd/source-harvester \
  --provider arxiv --category cs.IR --query 'retrieval augmented generation' \
  --from 2023-01-01 --through 2026-08-31 --limit 4 --page-size 2 \
  --output /tmp/tr-arxiv-new-run
```

`--from/--through` ограничивают дату публикации у научных источников и дату создания
репозитория у GitHub — это разные даты. `--work-type` у Crossref: `journal-article` (по
умолчанию), `proceedings-article`, `posted-content`. `--category` понимает только arXiv.

## Рабочий сбор

`--ingest` ведёт аренду поставщика, курсор и отправку в очередь через db-service, страницы
кладёт в S3. Нужны `HARVEST_OWNER_ID`, `DB_SERVICE_ADDR`, `RABBITMQ_URL` и `S3_*`. В Compose
процесс запущен как `--ingest --schedule /etc/harvester/scopes.json --timeout 15m`; расписание —
[infra/signalgraph-scopes.json](../../infra/signalgraph-scopes.json), интервал —
`HARVEST_INTERVAL` (6h).

- `--once` делает один ограниченный проход только по явно переданным scope.
- Тестовый прогон: `--once --test-run-id tr-test-… --manifest /новый/файл.json`, не больше 50
  записей. Манифест пишется до коммита в БД и перечисляет id документов и ключи S3. Живая
  проверка и очистка — `internal/worker/live_test.go` (`TR_SOURCE_PIPELINE_MANIFEST`,
  `TR_SOURCE_CLEANUP=1`).
- `--coverage --schedule <файл>` печатает покрытие по scope: стабильный манифест
  `sources/coverage/<хеш настройки scope>.json` и отчёт прогона
  `sources/coverage/runs/<run-id>.json`.
- Запросы пользователей (`POST /api/v1/trends/collections`) харвестер забирает через `Requested`
  рядом с настроенными scope. Адрес вне `COLLECTION_ALLOWED_DOMAINS` (настройка db-service)
  отклоняется ещё при постановке. Если контур управления недоступен, расписание всё равно
  выполняется.

### Курсор и бюджет

- Проход — до 200 записей и 15 минут, только целыми страницами. Незавершённый проход продолжает
  курсор, завершённый повторяет окно перекрытия.
- В scope расписания задаётся `current_month` или `history_months` (0..36), но не оба сразу.
  Незавершённое окно дня или месяца держится, пока не исчерпан его курсор: иначе новый день
  бросил бы вчерашний недобор.
- Страница с ошибкой запроса или записи курсор не двигает. Процесс с истёкшей арендой страницу
  не закоммитит.
- 409 от сборщика означает, что сменилась популяция источника. Курсор scope сбрасывается, и
  следующий проход идёт заново; дубли отсекаются по неизменяемым ревизиям. В отчёте —
  `source_snapshot_changed; cursor reset, restart next batch`.
- `budget_exhausted` — остановка на бюджете с сохранённым курсором. `search_exhausted` и
  `complete` — конец выдачи поставщика по этому запросу, а не полнота области. Состояние
  `provider_incomplete` и ограничения записей нельзя превращать в ноль.
- Успешная отправка не значит, что документ в индексе: статус `indexed` ставит chunk-splitter.

## Поставщики: лимиты и капризы

### arXiv

- Метаданные идут через OAI-PMH `arXivRaw`: `ARXIV_API_URL=https://oaipmh.arxiv.org/oai` (по
  умолчанию). Старый Atom `/api/query` работает, если явно задать такой адрес; курсоры двух
  форматов не смешиваются, у Atom он не дальше 30 000.
- Не чаще одного запроса в 3 секунды и одно соединение на адаптер. Один адаптер обслуживает все
  scope arXiv в процессе. Параллельные запуски и старые скрипты не должны обходить этот лимит.
  Таймаут запроса — 2 минуты, чтобы не оборвать ответ раньше шлюза.
- Размер страницы OAI выбирает сам arXiv, сейчас до 1300 записей. Ответ режется на локальные
  страницы, курсор хранит позицию внутри ответа и замечает, если страница изменилась. Запрос по
  resumptionToken не должен повторять исходные параметры — OAI это запрещает.
- Даты OAI — это даты обновления метаданных, а не публикации. Поэтому адаптер отдельно сверяет
  с окном дату v1 и дату полученной версии. Бюджет считает все просмотренные записи, включая
  отклонённые обновления. В расписании `page_size=100`, проход — 200 записей.
- `--category` (в JSON — `"category"`) задаёт одну категорию вместе с cross-list. Пустая
  категория сохраняет прежние scope ID.
- PDF берутся из массовой выгрузки arXiv в Google Cloud (`ARXIV_PDF_MIRROR`). arXiv сам
  предлагает её для массового скачивания, лимит «запрос в 3 секунды» к ней не относится, и файлы
  страницы качаются параллельно. Текущий месяц появляется там с опозданием, его PDF идут с
  arxiv.org через общий ограничитель. Сеть стенда рвёт HTTP/2, поэтому к зеркалу ходим по
  HTTP/1.1 с таймаутом 8 секунд.
- Atom хранит `published` (первая версия) и `updated` (полученная версия); DTD запрещён.
- Старый загрузчик `scripts/tools/data/ingest_arxiv_corpus.py` живой сбор не делает, у него
  остался только `--upload-only` для уже скачанных файлов.

### Crossref

- Не чаще раза в секунду; заголовки `X-Rate-Limit-*` и `Retry-After` могут только замедлить
  темп. `PUBSEARCH_MAILTO` необязателен.
- Точность `date-parts` сохраняется: частичная дата, которая пересекает границу окна,
  отклоняется. Сортировка по score — релевантность поставщика, а не рейтинг тренда.
- Crossref [изменил поведение курсоров](https://community.crossref.org/t/changes-to-cursors-filtering-and-sorting-in-the-rest-api/16246).
  Перекрытие страниц ловит общая проверка прогресса: новые записи из перекрытия сохраняются без
  повторной отправки, а застрявший курсор или точный повтор перезапускает окно. Покрытие после
  перекрытия помечается неполным.
- Полный текст ищется по точному DOI в Europe PMC. Открытый PMCID читается как исходный JATS
  XML, и DOI сверяется ещё раз внутри XML. Ключи не нужны, поставщиком записи остаётся
  `crossref`. Рисунки, вёрстка формул и внешние приложения не переносятся. В расписании
  `page_size=1`, чтобы обогащённая статья помещалась в конверт S3 и RPC.
- Меняющиеся счётчики цитирований новую ревизию документа не создают.

### GitHub

- Не чаще раза в 6 секунд. При `X-RateLimit-Remaining: 0` адаптер ждёт `X-RateLimit-Reset`.
- Поиск отдаёт не больше 1000 результатов на запрос, поэтому неполнота пишется, даже если
  страницы кончились. На странице — до 10 репозиториев с обогащением; в рабочем сборе
  `page_size=1`, чтобы ответ укладывался в лимит RPC.
- `GITHUB_TOKEN` необязателен. Он не попадает в манифесты и не уходит по редиректу. Приватные
  репозитории и репозитории с неподтверждённой видимостью отклоняются.
- README читается по commit SHA не позже границы окна. Чужой код не клонируется и не
  исполняется. Звёзды — текущее наблюдение с датой, а не история. `--sort updated|stars` — это
  порядок сбора, а не оценка тренда.

### patent и web

- Записи читаются через `/v1/records` у `PATENT_COLLECTOR_URL` (по умолчанию
  `http://patent-collector:8090`) и `WEB_COLLECTOR_URL` (`http://web-collector:8090`). Scope
  `country:CN` уходит в `CN_PATENT_COLLECTOR_URL`; в расписании у него `history_months=13`, потому
  что китайские публикации приходят с задержкой.
- Ревизия `collector-v2:<sha256>` считается по фактам источника. Исправленные даты подачи и
  приоритета, семейство, приоритетная заявка или исходный URL дают новую ревизию, даже если текст
  не изменился. Время наблюдения, версия парсера и порядок полей JSON новой ревизии не дают.
- `source_metadata.text_status`: `full_text`, `article_text`, `pdf`, `abstract_only`,
  `title_and_abstract`, `title_only`, `representative_claim`, `metadata_only`, `pinned_readme`,
  `repository_metadata_only`. У старых источников поле пустое — это «неизвестно».
  `representative_claim` — не полное описание патента, `title_only` — только метаданные.

## Язык материала

Язык, который назвал поставщик, не переписывается. Иначе его определяет `whatlang-v1.0.1` по
всему тексту, и версия детектора пишется в `language_basis`. Если текста меньше 40 кодовых
точек или детектор не уверен, запись получает ограничение `language_undetermined`. Язык не
условие допуска: оригинал на любом языке индексируется и идёт в извлечение.

## Ручной импорт

`POST /api/v1/documents` принимает multipart `file` и заголовок `X-Document-Source` с манифестом
`sourcedoc` v1 до 32 KiB; без заголовка это обычная загрузка. Файл — до 8 MiB: PDF, непустой
UTF-8 текст или Markdown, HTML. Хеш из манифеста сверяется с байтами до записи в S3. URL
сохраняется как происхождение, сервер по нему не ходит. Повтор того же файла и манифеста
возвращает существующий документ с `duplicate=true`. Ручной импорт идёт под `provider=manual` и
не входит в покрытие автоматического сбора.

Операторский путь — [import_signalgraph_source.py](../../scripts/tools/data/import_signalgraph_source.py).
Токен берётся из `SIGNALGRAPH_TOKEN`, `--receipt` — новый файл, существующий не
перезаписывается.

```sh
python3 scripts/tools/data/import_signalgraph_source.py \
  --api http://host:5173/api/v1 --file /abs/RU2789123.pdf \
  --source /abs/RU2789123.source.json --receipt /abs/new-receipt.json
```

Патенту нужен блок `patent`: `publication_number`, `jurisdiction`, `kind_code`; по желанию
`application_number`, `family_id` вместе с `family_basis` и `translation_of`. Перевод получает
`canonical_id` оригинала и второго изобретения не создаёт. Одна публикация в одной юрисдикции —
одна запись.

```json
{
  "version": 1,
  "source_kind": "patent",
  "provider": "manual",
  "access_scope": "owner",
  "native_id": "RU2789123",
  "canonical_id": "patent:RU2789123",
  "revision": "ru-2789123-c1",
  "content_hash": "<sha256 файла>",
  "hash_algorithm": "sha256",
  "observed_at": "2026-09-12T00:00:00Z",
  "url": "https://patents.example.org/RU2789123",
  "publication": {
    "value": "2024-11-20",
    "precision": "day",
    "basis": "титульный лист публикации"
  },
  "patent": {
    "publication_number": "RU2789123",
    "jurisdiction": "RU",
    "kind_code": "C1",
    "application_number": "2023101234",
    "family_id": "ru-2023101234",
    "family_basis": "та же заявка; шире семейство неизвестно"
  }
}
```

## Проверка

```sh
go test -race ./...
golangci-lint run ./...
```

Записанные ответы Crossref лежат в `internal/provider/testdata/crossref`, остальные фикстуры
синтетические. Живые проверки включаются переменными: `TR_LIVE_SOURCES=1` и
`TR_SOURCE_PROOF_DIR=<каталог>` сохраняют сырые ответы, например
`go test ./internal/provider -run '^TestLiveEuropePMCFullText$' -v -count=1`.
