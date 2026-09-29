# paddleocr-vl-service

Единственный OCR-движок контура: `ocr-service` шлёт сюда сканы и изображения
(`OCR_ENGINE_URL=http://paddleocr-vl-service:8088`, `OCR_MODEL=PaddleOCR-VL`) и получает
Markdown с порядком чтения, разделами, таблицами и формулами — структура помогает дальнейшему
разбиению на фрагменты.

## Контракт

`POST /`:

```json
{"model":"PaddleOCR-VL","image_b64":"...","mime":"application/pdf"}
```

Ответ:

```json
{"text":"...","pages":["page 1 markdown", "page 2 markdown"]}
```

PDF распознаётся постранично, `text` — страницы, склеенные в порядке чтения. Пределы: до
16 МиБ на входе, до 128 страниц, до 16 МиБ текста на выходе; запросы идут по одному.
`GET /health` показывает устройство, готовность и backend распознавания.

## Распознавание в llama.cpp

Сервис сам только размечает страницу (PP-DocLayoutV3), а распознаёт её `paddleocr-llama` —
та же PaddleOCR-VL-1.6 в GGUF. Так она занимает 2,4 ГБ видеопамяти вместо 8,3 ГБ у
нативного `paddle_dynamic`, который держал память и в простое, и разбирает страницу за
1–2 с вместо 6–8. Веса кладутся рядом с GGUF генерации:

```bash
hf download PaddlePaddle/PaddleOCR-VL-1.6-GGUF --local-dir models/gguf/paddlevl
```

Разметка скачивает свои веса в `models/paddlex` при первом старте.

| Переменная | Compose | Смысл |
| --- | --- | --- |
| `PADDLEOCR_VL_DEVICE` | `gpu` | устройство разметки |
| `PADDLEOCR_VL_REC_BACKEND` | `llama-cpp-server` | backend распознавания |
| `PADDLEOCR_VL_REC_SERVER_URL` | `http://paddleocr-llama:8099/v1` | OpenAI-совместимый адрес backend-а |
| `PADDLEOCR_VL_REC_MAX_CONCURRENCY` | пусто | параллельных запросов адаптера к backend-у |

Backend и адрес задаются только парой: одно без другого — отказ при старте. Вернуть
нативный `paddle_dynamic` пустыми переменными окружения нельзя: `${…:-…}` в Compose
подставит значение по умолчанию и вместо пустой строки. Нужен оверлей
(`infra/docker-compose.local.yml`), где обе переменные пустые, а зависимость от
`paddleocr-llama` снята.

Для больших партий есть профиль `ocr-vllm` с `paddleocr-vlm-server`:
`PADDLEOCR_VL_REC_BACKEND=vllm-server`,
`PADDLEOCR_VL_REC_SERVER_URL=http://paddleocr-vlm-server:8118/v1`. vLLM лучше
пакетирует запросы, но резервирует долю видеопамяти из
[infra/paddleocr-vllm.yml](../../infra/paddleocr-vllm.yml) — её подбирают вместе с
остальными GPU-сервисами.

## Запуск

```bash
docker compose -f infra/docker-compose.yml up -d paddleocr-vl-service
```

Без GPU — оверлей [infra/docker-compose.nogpu.yml](../../infra/docker-compose.nogpu.yml).
