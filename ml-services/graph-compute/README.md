# graph-compute

Rust-сервис графовых расчётов без состояния: читает векторы из Qdrant (или получает их в
запросе), строит kNN-граф по косинусу, находит сообщества Leiden (Louvain — полем `algorithm` запроса) и
возвращает результат вызывающему. Каждый вызов полностью определяется запросом.

## RPC

Контракт — [graph.proto](../../backend/proto/graph/v1/graph.proto).

| RPC | Кто вызывает | Что делает |
| --- | --- | --- |
| `Cluster` | `cluster-worker`, `raptor-worker` (глобальный режим) | темы по документам (`document`) или по фрагментам (`chunk`): состав, представительные документы, метрики, преемственность с прошлой доской |
| `ClusterSnapshot` | `signal-worker` | кластеры только по переданным проверенным векторам работ, без чтения Qdrant и прошлой доски |
| `ScoreBridges` | `discovery-worker` (режим `bridge`) | пары тем, близких по смыслу, но слабо связанных напрямую, с документами-посредниками |
| `Rank` | `llm-service` при `RAG_AGENTIC_GRAPH=true` | Personalized PageRank по тому же графу от затравочных документов |
| `Paths` | — | пути между двумя наборами узлов |

HTTP-зеркало: `POST /v1/cluster`, `/v1/bridges`, `/v1/rank`, `/v1/paths` (JSON с именами
полей proto), `GET /healthz`, `GET /metrics`. При `APP_ENV=production` остаются только
`/healthz` и `/metrics`, а gRPC требует `GRPC_TLS_MODE=mtls` с `GRPC_TLS_CA`,
`GRPC_TLS_CERT`, `GRPC_TLS_KEY`.

## Настройки

| Переменная | По умолчанию | Смысл |
| --- | --- | --- |
| `GRPC_ADDR` | `0.0.0.0:9093` | gRPC |
| `HTTP_ADDR` (или `METRICS_ADDR`) | `0.0.0.0:8080` | HTTP-зеркало и метрики |
| `QDRANT_URL`, `QDRANT_COLLECTION` | `http://qdrant:6333`, `documents` | источник векторов |
| `LEGACY_ALLOW_SNAPSHOT_SOURCES` | выкл., в Compose `true` | пускать в `Cluster` документы из снимков источников |

Без `LEGACY_ALLOW_SNAPSHOT_SOURCES` в доску тем попадают только загруженные вручную файлы, а
всё собранное роботом отбрасывается — доска показывает малую и перекошенную часть корпуса.
Доступ при этом не ослабляется: список документов уже отфильтрован API по владельцу.

Граф держит в памяти по два ведущих фрагмента на документ; на корпусе в 25 тыс.
документов это около 200 МБ векторов, в Compose лимит 4 ГБ.

## Сборка и тесты

```bash
cargo build --features service --bins
cargo clippy --all-targets --all-features -- -D warnings
cargo test --all-features
```

Код: `src/domain` — чистые алгоритмы, `src/application` — сервис над источником векторов и
детектором сообществ, `src/infrastructure` — Qdrant и Leiden/Louvain, `src/interfaces` —
gRPC и HTTP.
