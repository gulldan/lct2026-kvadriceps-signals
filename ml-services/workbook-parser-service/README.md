# workbook-parser-service

Структурный разбор таблиц Excel и других книг на Rust (`calamine`): `.xls` и современные
форматы читаются без LibreOffice. Вызывает `office-parser` (`WORKBOOK_PARSER_URL`); если
сервис ответил ошибкой, книга читается плоским текстом через excelize, а Tika остаётся для
неизвестных форматов.

## API

```bash
curl -F "file=@book.xlsx" http://localhost:8095/v1/parse
```

Ответ:

- `text` — Markdown-окна таблиц с якорями `source_uri=...`;
- `metadata` — поля для `DocumentParsed.metadata`;
- `sidecars` — JSON с привязкой к ячейкам, сохраняется рядом с текстом.

Нечитаемая книга — 422. `GET /health` — готовность.

Каждая книга разбирается в отдельном процессе с пределами 60 с процессорного времени и
512 МиБ адресного пространства (на Linux): разреженные XLS/XLSX и повторы ODS раздувают
память внутри библиотеки раньше, чем их можно проверить.

## Настройки

- `HTTP_ADDR` — адрес, по умолчанию `0.0.0.0:8095`;
- `WORKBOOK_MAX_UPLOAD_MB` — предел загрузки, по умолчанию и не больше 128.

В S3, RabbitMQ и Postgres сервис не пишет: хранение и очередь — у `office-parser`.

## Проверка

```bash
cargo test
cargo clippy --all-targets -- -D warnings
```
