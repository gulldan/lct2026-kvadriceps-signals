# raptor-worker

Строит обзорные узлы RAPTOR над фрагментами документов и пишет их в ту же коллекцию Qdrant,
чтобы поиск мог ответить на широкий вопрос. В ответе пользователь всё равно видит исходные
фрагменты, а не обзор. Нужна модель генерации: без `VLLM_URL` воркер простаивает.

## Режимы

`RAPTOR_MODE=incremental` (по умолчанию): после смены эпохи корпуса и паузы `DEBOUNCE_SEC`
воркер берёт новые и изменённые документы, из ограниченной выборки их фрагментов модель
пишет краткий обзор, `f2llm-service` строит его вектор, и в Qdrant ложится точка
`node_type=raptor_summary`. Идентификатор точки стабилен: повторный запуск обновляет запись,
а не плодит дубль. Глобальный граф не строится.

Любое другое значение — глобальный режим: кластеры фрагментов через `Cluster` в
`graph-compute` (гранулярность `chunk`). Он для офлайн-опытов и на большом корпусе медленный.

В точке обзора: `node_level`, `member_ids` (исходные фрагменты) и `members` — копии
фрагментов для цитирования.

## Настройки

Значения по умолчанию — в `infra/docker-compose.yml`. Неочевидное:

| Переменная | Смысл |
| --- | --- |
| `RAPTOR_INCREMENTAL_BATCH`, `RAPTOR_INCREMENTAL_WORKERS` | документов за проход и параллельных построителей |
| `RAPTOR_DOC_MAX_CHUNKS` | фрагментов на обзор документа |
| `RAPTOR_SUMMARY_MAX_TOKENS` | длина обзора |
| `RAPTOR_MAX_MEMBERS_FOR_SUMMARY`, `RAPTOR_SUMMARY_MEMBERS` | фрагментов в запросе к модели и копий в `members` |
| `RAPTOR_MAX_LEVEL`, `RAPTOR_INPROC_MAX_NODES` | только глобальный режим |
| `AUTO_RAPTOR` | главный выключатель |

## Проверка

```bash
docker compose -f infra/docker-compose.yml exec -T raptor-worker python worker.py --selfcheck
docker compose -f infra/docker-compose.yml logs --tail=120 raptor-worker
```

Ожидается: `rag:raptor:last_epoch` совпадает с `rag:corpus_epoch:shared`, в Qdrant есть точки
`node_type=raptor_summary`, ответы RAG цитируют исходные фрагменты, а не обзор.
