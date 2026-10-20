# signal-worker

Считает анализ трендов по собственному корпусу для `POST /trends/analyses`: одно
ограниченное задание за раз, без LLM.

## Как работает

1. `main-service` создаёт задание в `db-service` и замораживает вход (canonical JSON +
   SHA-256, копия в S3).
2. Воркер забирает задание через `Claim` по SHA-256 своего `method.json` и продлевает lease
   каждые 30 секунд.
3. Перед расчётом и перед публикацией вход перечитывается и сверяется с артефактом S3;
   расхождение — ошибка, а не пересчёт.
4. `signalgraph/pipeline.py`: кандидаты — `ClusterSnapshot` в `graph-compute` (Leiden по
   эмбеддингам F2LLM), измерения по кварталам, релевантность запросу, проблема, преимущество
   и пример из цитат источников.
5. В ранжирование входят только кандидаты с `complete_for_brief` и подтверждённой
   релевантностью. MMR сочетает временной порядок с различием эмбеддингов; повтором
   считается косинусная близость от `ranking_duplicate_threshold`. Итог — `complete`
   (ровно 15) или `insufficient_coverage` с фактическим числом.
6. Метод и результат пишутся в S3 (`signalgraph/v1/...`), затем `Validate` и `Publish`.
   Потеря lease или сигнал остановки отменяют публикацию.
7. Ошибка уходит в `Fail` с кодом: `access_changed`, `invalid_input`, `invalid_result` — без
   повтора; `compute_unavailable`, `worker_failure` — повтор, не больше трёх попыток.

Параметры кластеризации, окон, извлечения цитат и MMR лежат в `method.json`; метод имеет
`quality_status=not_frozen`: параметры ещё не заморожены.

`ranking.diversity` хранит версию MMR, параметры, хеш входа и исключённые ID. Go-контур
независимо проверяет порядок и исключения по закреплённым снимкам индекса; без этой
проверки результат нового метода не публикуется. Векторы и сходства в публичный результат
не попадают; старые результаты читаются по своему контракту.

## Пределы

| Что | Предел |
| --- | --- |
| Вход | 2 000 документов, 64 МиБ JSON |
| Исходный текст документа | 16 МиБ |
| Снимок векторов | 128 МиБ |
| Всё прочитанное за задание | 4 ГиБ — одинаково в Python и в Go-проверке |
| История сбора | до 64 страниц, до 16 МиБ на манифест и на ответ страницы |
| Результат | 16 МиБ |

4 ГиБ — объём прочитанного, а не памяти; корпус из 1 604 работ читается примерно за 2 ГиБ.
Если история сбора не влезает в пределы, её страницы не читаются, полнота
покрытия по периодам остаётся неизвестной, и это видно в `coverage.read_status`,
ограничении `collection_coverage_not_read_page_budget` и интерфейсе. Проверка самих
источников при этом не ослабляется.

Общие для всех кварталов списки хранятся в результате один раз:
`measurements.unscoped_scientific_work_ids` и `measurements.scientific_scope_ids`, а в
`series[]` — только проверенные области квартала (`covered_scientific_scope_ids`).
В CSV общие списки уходят в `measurement_context.profile`, квартальные — в
`measurement_context.period`.

## Настройки

| Переменная | Compose | Смысл |
| --- | --- | --- |
| `DB_SERVICE_ADDR` | `db-service:9091` | gRPC `db-service` |
| `GRAPH_COMPUTE_ADDR` | `graph-compute:9093` | кластеризация снимка |
| `EMBEDDINGS_URL` | `http://f2llm-service:8085/v1/embeddings` | эмбеддинги |
| `S3_*` | общее хранилище | артефакты входа и результата |
| `DB_READY_URL` | `http://db-service:9090/readyz` | готовность `db-service` для healthcheck |
| `SIGNALGRAPH_METHOD_FILE` | `method.json` | путь к методу |

## Запуск и проверка

Сервис в профиле `signalgraph`:
`docker compose -f infra/docker-compose.yml --profile signalgraph up -d signal-worker`.
Локально из `ml-services/signal-worker` с `PYTHONPATH=../runtime`:

```sh
python worker.py --once          # одно задание и выход
python worker.py --print-method  # canonical JSON метода
python worker.py --healthcheck   # прогресс + готовность DB, S3, graph-compute, эмбеддингов
python -m unittest discover -s tests
```

После правки `worker.py`, `signalgraph/*.py` или `requirements.txt` пересоберите
`method.json` через `--print-method` в окружении с теми же зависимостями. Одни и те же байты
должны лежать в `ml-services/signal-worker/method.json` и
`backend/platform/pkg/analysisdoc/method.json`, затем — `make vendor`; `test_method.py`
сверяет артефакт с исходниками. API и воркер выкладываются вместе: задания привязаны к хешу
метода, старые результаты не переписываются.

## Чего воркер не делает

Не вызывает LLM, не меняет документы и индексы, не подставляет текущий метод в старое
задание, не ранжирует по звёздам и не складывает работы с репозиториями и релизами.
