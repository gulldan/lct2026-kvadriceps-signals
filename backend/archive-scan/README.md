# archive-scan

Rust-сервис и CLI для архивов. В основном контуре распаковывает архивы для `archive-worker`
(`ARCHIVE_SCAN_URL=http://archive-scan:3000`): извлечённые файлы пишутся в S3, а
`archive-worker` регистрирует их как документы. В Go-цели Makefile не входит.

## Сборка и проверки

```bash
cargo build --release --bin archive_scan               # только CLI
cargo build --release --features service --bins        # CLI и HTTP-сервис archive_scan_server
cargo test --all-features
cargo clippy --workspace --all-targets --all-features --no-deps -- -D warnings
```

Интеграционные тесты используют Docker, если он доступен. Образ (`Dockerfile`) запускает
`archive_scan_server` на порту 3000.

## CLI

```bash
# метаданные содержимого архивов
./target/release/archive_scan /data/archives --fast-only \
  --out-ndjson rows.ndjson --out-parquet rows.parquet

# распаковка
./target/release/archive_scan extract /data/archives \
  --out-dir /data/extracted --fast-only --metadata-ndjson extracted.ndjson
```

`--fast-only` определяет тип файла только по сигнатуре, без модели Magika. `--full-hash`
считает полный BLAKE3 содержимого — это дорого. У каждого флага есть переменная окружения
`ARCHIVE_SCAN_*`.

## HTTP-сервис

Локально, с хранилищем на диске:

```bash
ARCHIVE_SCAN_SERVICE_ADDR=127.0.0.1:3000 \
ARCHIVE_SCAN_EXTRACT_STORE_BACKEND=filesystem \
ARCHIVE_SCAN_EXTRACT_STORE_DIR=/tmp/archive-scan/extracted \
ARCHIVE_SCAN_EXTRACT_METADATA_BACKEND=filesystem \
ARCHIVE_SCAN_EXTRACT_METADATA_DIR=/tmp/archive-scan/metadata \
./target/release/archive_scan_server

curl -sS http://127.0.0.1:3000/v1/extract/upload \
  -F archive=@sample.zip -F fast_only=true -F include_entries=false | jq .
```

Маршруты и схемы — `GET /openapi.json` и `GET /docs`; есть `/healthz`, `/readyz` и `/metrics`.
Настройки основного контура — в сервисе `archive-scan` файла
[infra/docker-compose.yml](../../infra/docker-compose.yml). Там файлы пишутся в S3, метаданные
извлечения не хранятся, а размер архива ограничивает `ARCHIVE_SCAN_OBJECT_SOURCE_MAX_BYTES`.

Задачи `/v1/jobs` и их результаты по умолчанию пишутся на диск контейнера и пропадают вместе с
ним. Чтобы они переживали пересоздание, задайте `ARCHIVE_SCAN_JOB_STORE_BACKEND=postgres` (или
`redis`) и `ARCHIVE_SCAN_RESULT_STORE_BACKEND=s3`. Пример — в
[deploy/kubernetes/archive-scan.yaml](deploy/kubernetes/archive-scan.yaml).
