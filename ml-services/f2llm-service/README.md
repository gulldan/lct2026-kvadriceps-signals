# f2llm-service

Смысловые векторы текста в формате OpenAI: `POST http://f2llm-service:8085/v1/embeddings`.
Им пользуются все, кому нужны эмбеддинги: индексация (`chunk-splitter`), поиск
(`llm-service`), радар, воркеры и `extractor`; в Compose адрес — `EMBEDDINGS_URL` и его
аналоги у воркеров.

## Модель

`codefuse-ai/F2LLM-v2-0.6B`: вектор 1024, пулинг по последнему токену, L2-нормирование; на
CUDA — bf16. Модель задаёт пространство векторов: после смены модели или размерности весь
корпус нужно переиндексировать, векторы разных моделей несравнимы.

Ответ и `GET /health` несут `revision` — коммит весов, способ пулинга, `F2LLM_MAX_TOKENS`,
тип данных и версии torch/transformers. Индексация со снимками источников
(`SOURCE_INDEX_SNAPSHOTS=true`) сверяет её с `EMBEDDINGS_REVISION` и отказывает при
расхождении, поэтому после любой из этих смен обновите `EMBEDDINGS_REVISION` в `infra/.env`
по фактическому `/health`.

Запрос — от 1 до 128 текстов, до 64 КиБ на текст и 512 КиБ всего.

## Настройки

| Переменная | Compose | Смысл |
| --- | --- | --- |
| `F2LLM_MODEL`, `F2LLM_REVISION` | модель выше, пусто | модель и закреплённый коммит весов; несовпадение коммита — отказ при старте |
| `F2LLM_MAX_TOKENS` | 512 | длина входа, до 4096 |
| `F2LLM_BATCH` | 8 | текстов за проход модели, до 32 |
| `TORCH_DEVICE` (`F2LLM_TORCH_DEVICE`) | `cuda` | `auto` выбирает cuda, mps или cpu |

`EMBEDDINGS_MODEL` и `EMBEDDING_DIM` — настройки клиентов, а не сервиса; они должны
совпадать с моделью.

## Запуск

```bash
docker compose -f infra/docker-compose.yml up -d f2llm-service
```

При первом старте модель скачивается в `models/huggingface`. Без GPU — оверлей
[infra/docker-compose.nogpu.yml](../../infra/docker-compose.nogpu.yml).
