# radar-analysis

Отдельный Go-процесс анализа живого радара. Получает датированный снимок работ, находит смысловые
кандидаты, считает всплески и доказательства. В базу и объектное хранилище не пишет: снимки
публикует `radar-collector`. Место в системе — в [docs/ARCHITECTURE.md](../../../../docs/ARCHITECTURE.md).

Сборка из `backend/main-service`: `go build -o radar-analysis ./cmd/radar-analysis`.
`/app healthcheck` проверяет `127.0.0.1:8080`, то есть порт по умолчанию.

| Переменная            | По умолчанию                              |
| --------------------- | ----------------------------------------- |
| `RADAR_ANALYSIS_ADDR` | `:8080`                                   |
| `EMBEDDINGS_URL`      | `http://f2llm-service:8085/v1/embeddings` |
| `EMBEDDINGS_MODEL`    | `codefuse-ai/F2LLM-v2-0.6B`               |
| `OPENALEX_API_URL`    | `https://api.openalex.org`                |
| `OPENALEX_API_KEY`    | необязателен                              |
| `GITHUB_TOKEN`        | необязателен                              |

## API

- `GET /healthz` — процесс жив.
- `POST /v1/analyze` принимает `LiveAnalysisRequest` и возвращает `LiveAnalysisResult` (оба — в
  `internal/application/trend_live_analysis.go`).
- `POST /v1/evaluate` принимает `{pairs, threshold}`; у пар обязательны метки, их источник и
  разбиение.

Одновременно идёт один анализ, второй запрос получает 409. Предел — 14 минут и тело до 512 MiB,
в снимке до 200 000 работ. Для поиска кандидатов берётся до 12 288 работ: выборка
стратифицирована по месяцам и категориям и воспроизводима, её счётчики — не оценка всей
популяции. Если модель или транспорт отказали, лексического запасного пути нет: анализ отвечает
502, и сборщик оставляет прежнее наблюдение.

## Флаги запроса

- `strict_availability=true` — исторический срез: работы без версий исключаются, OpenAlex и
  GitHub не опрашиваются, поздние аффилиации не подмешиваются.
- `enrich_metadata=true` — за проход до 64 поисков аффилиаций в OpenAlex по точному DOI и до 32
  проверок форков GitHub, не чаще запроса в секунду. Проверенное не перепроверяется 7 дней.
  Отказ поставщика оставляет прежние доказательства. Время этих наблюдений пишется отдельно в
  `evidence.observed_at`: аффилиация, увиденная сегодня, может описывать работу из прошлого окна.
- `evidence_only=true` — только карта доказательств по темам, без модели и OpenAlex. Нужен после
  замены патентных и веб-записей.

OpenAlex ходит по HTTP/1.1: тот же запрос по DOI зависал на HTTP/2. Названия организаций не
склеиваются в одну сущность — связь даёт только id поставщика.

## Диагностика по сохранённому снимку

Команды читают stdin (до 64 MiB) и не обращаются к модели, базе, сборщику и хранилищу:

```sh
radar-analysis evaluate-burst < snapshot.json
radar-analysis calibrate-burst < snapshot.json
```

`evaluate-burst` — причинный бэктест всплеска `coverage-adjusted-monthly-residual-v1`: детектор
видит только прошлое до целевого месяца. `calibrate-burst` считает биномиальные вероятности при
неизменной, двойной и четверной доле. Это инженерная калибровка, а не документальные метки.
Пороги детектора обе команды не меняют.

## Проверка

```sh
go test -race ./internal/application ./internal/infrastructure/trendanalysis ./internal/infrastructure/trendsemantic ./cmd/radar-analysis
go vet ./internal/application ./internal/infrastructure/trendanalysis ./internal/infrastructure/trendsemantic ./cmd/radar-analysis
```
