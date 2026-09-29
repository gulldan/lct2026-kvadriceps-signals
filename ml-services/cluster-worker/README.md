# cluster-worker

Перестраивает доску тем, когда корпус изменился и загрузка затихла, — на готовых векторах
Qdrant, без повторного эмбеддинга.

## Как работает

1. `main-service` после индексации увеличивает `rag:corpus_epoch:shared` в Valkey.
2. Воркер ждёт `DEBOUNCE_SEC` без новых изменений и вызывает `Cluster` в `graph-compute`.
3. Темы подписывает модель генерации (`VLLM_*`, ответ до 1024 токенов, учитывает
   `VLLM_REASONING_EFFORT`), а без неё — содержательные фразы документов, включая
   исходные китайские заголовки. `labels.py` не даёт служебным полям публикаций (`URL`,
   `Source`, `Language`, DOI, издатель) стать названием темы.
4. Темы публикуются через `POST /clusters/replace`. Преемственность с прошлой доской
   строится по `GET /clusters?view=full`: компактная проекция не содержит состава тем.
5. Воркер ставит `rag:itc:trigger` для пересчёта ITC и при `AUTO_HYPOTHESES` создаёт
   базовые гипотезы по крупным темам.

Если новый расчёт не вернул тем, старая доска остаётся.

## Настройки

Значения по умолчанию и почему они такие — в `infra/docker-compose.yml`. Неочевидное:

| Переменная (в Compose) | Смысл |
| --- | --- |
| `CLUSTER_AUTORUN` | `false` оставляет воркер живым, но выключает автоматическую перекластеризацию и переразметку тем моделью при каждой смене эпохи |
| `RESOLUTION`, `MIN_SIZE` (`CLUSTER_RESOLUTION`, `CLUSTER_MIN_SIZE`) | крупность тем; малая резолюция с большим минимумом даёт меньше осколков |
| `KNN_K`, `SIM_THRESHOLD`, `MUTUAL_KNN` | построение графа ближайших соседей |
| `LABEL_REUSE`, `LABEL_REUSE_STABILITY` | сохранять название темы, если её состав почти не изменился, — не тратить вызовы модели |
| `VLLM_RPM` (`CLUSTER_VLLM_RPM`) | темп разметки, чтобы массовая переиндексация не забила генерацию |
| `GRAPH_COMPUTE_TIMEOUT`, `RUN_TIMEOUT` (`CLUSTER_RUN_TIMEOUT`) | срок вызова графа и всего прогона, секунды; на большом корпусе прогон идёт часами |
| `AUTO_HYPOTHESES`, `AUTO_HYPOTHESIS_LIMIT` | базовые гипотезы по темам и их число |

## Проверка

Загрузите документы, дождитесь статуса indexed и откройте `/summary`. Логи и проверки без
внешних запросов (из `ml-services/cluster-worker`):

```bash
docker compose -f ../../infra/docker-compose.yml logs --tail=120 cluster-worker
PYTHONPATH=../runtime python worker.py --selfcheck
PYTHONPATH=../runtime python -m unittest -v test_labels.py
```
