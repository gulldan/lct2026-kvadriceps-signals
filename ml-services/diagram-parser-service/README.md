# diagram-parser-service

Локальный разбор технологических схем: принимает тот же запрос описания изображения, что
`vlm-service` шлёт внешнему VLM, выделяет блоки и связи средствами OpenCV, распознаёт
подписи и возвращает текст для индексации. `vlm-service` направляют сюда через
`IMAGE_PARSE_ROUTE=vlm`, `VLM_ENGINE_URL=http://diagram-parser-service:8089`,
`VLM_API_STYLE=native` и `VLM_MODEL=diagram-opencv-paddle`; сервис поднимается профилем
compose `diagram-parser`.

## Контракт

`POST /` с `{"model", "image_b64", "mime", "prompt"}` → `{"text"}`; только `image/*`, до
16 МиБ изображения. `GET /health` — готовность. Порт 8089.

## Два образа

| Сервис (профиль Compose) | Образ | Распознавание подписей |
| --- | --- | --- |
| `diagram-parser-service` (`diagram-parser`) | `Dockerfile` | внешний OCR по `DIAGRAM_OCR_URL`, по умолчанию `paddleocr-vl-service` |
| `diagram-parser-paddle5-service` (`diagram-parser-paddle5`) | `Dockerfile.paddle5` | PP-OCRv5 на GPU внутри контейнера, `DIAGRAM_OCR_URL` не нужен |

Режим OCR — `DIAGRAM_OCR_MODE`:

- `full-image` (по умолчанию) — вся схема одним запросом к внешнему OCR;
- `tiles4`, `tiles9` — схема режется на 4 или 9 перекрывающихся плиток;
- `blocks` — OCR каждого найденного блока; `hybrid` — блоки плюс вся схема;
- `adaptive` — блоки, если их не больше `DIAGRAM_BLOCK_OCR_MAX_BLOCKS`, иначе вся схема;
- `paddle5` — PP-OCRv5 локально, только в образе `Dockerfile.paddle5`;
- `none` — без OCR, только структура.

Все режимы, кроме `paddle5` и `none`, требуют внешнего OCR.

## Почему PP-OCRv5

PP-OCRv5 (`PP-OCRv5_server_det` + `eslav_PP-OCRv5_mobile_rec`) распознаёт кириллицу в
коротких подписях схем, а проверенный распознаватель PP-OCRv6 нужного покрытия кириллицы не
дал. Surya 2 требует отдельного vLLM и намного тяжелее. Paddle ставится из индекса CUDA 12.9
(`paddlepaddle-gpu==3.3.1`): колесо под CUDA 12.6 падало на RTX 5080 (Blackwell, `sm_120`),
а 12.9 работает и на Ada уровня RTX 4090 при свежем драйвере.

Настройки образа PP-OCRv5 (значения Compose):

```env
DIAGRAM_PADDLE5_LANG=ru
DIAGRAM_PADDLE5_DEVICE=gpu
DIAGRAM_PADDLE5_DET_MODEL=PP-OCRv5_server_det
DIAGRAM_PADDLE5_REC_MODEL=eslav_PP-OCRv5_mobile_rec
DIAGRAM_PADDLE5_REC_BATCH_SIZE=8
PADDLE_PDX_DISABLE_MODEL_SOURCE_CHECK=True
```

Веса кэшируются в `/root/.paddlex` (`models/paddlex` на хосте).

`DIAGRAM_FAIL_OPEN=true` вместо ошибки 502 возвращает текст `diagram_parse_error: …`;
`DIAGRAM_PARSE_CONCURRENCY` — одновременных разборов.

## Запуск и проверки

```bash
docker compose -f infra/docker-compose.yml --profile diagram-parser up -d diagram-parser-service vlm-service
docker compose -f infra/docker-compose.yml --profile diagram-parser-paddle5 up -d diagram-parser-paddle5-service vlm-service
```

Из корня репозитория:

```bash
uv run --project ml-services/diagram-parser-service ruff check ml-services/diagram-parser-service
PYTHONPATH=ml-services/diagram-parser-service \
  uv run --project ml-services/diagram-parser-service ty check ml-services/diagram-parser-service/diagram_parser_service
PYTHONPATH=ml-services/diagram-parser-service:ml-services/runtime \
  uv run --project ml-services/diagram-parser-service python -m unittest discover -s ml-services/diagram-parser-service/tests -v
```
