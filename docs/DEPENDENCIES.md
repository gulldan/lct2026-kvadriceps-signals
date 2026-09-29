# Зависимости

При запуске через Docker пакеты приложения устанавливаются во время сборки.
Ниже перечислены компоненты и файлы с точными версиями библиотек.

## Для запуска

Нужны Git, Make, Python 3, Docker 24+ и Docker Compose 2.24.4+. Команды запуска — в [руководстве по развёртыванию](deployment_guide.pdf).
Для GPU дополнительно нужен NVIDIA Container Toolkit; режим радара на ноутбуке работает без GPU.
Для предварительной загрузки файлов распознавания нужен Python-пакет `huggingface_hub` с командой `hf`.

Основные внешние компоненты и версии из [docker-compose.yml](../infra/docker-compose.yml):

| Компонент | Версия | Назначение |
| --- | --- | --- |
| PostgreSQL | 18.6 | Записи и результаты |
| SeaweedFS | 4.45 | Файлы |
| Qdrant | 1.19.1 | Поиск по смыслу |
| OpenSearch | 3.8.0 | Поиск по словам |
| RabbitMQ | 4.3.5 | Очередь задач |
| Valkey | 9.1.2 | Кэш |
| nginx | 1.31.5 | Доступ к интерфейсу и API |

## Библиотеки приложения

Полные списки пакетов и точные версии хранятся в файлах, которые использует сборка:

| Часть | Где зафиксированы зависимости |
| --- | --- |
| Интерфейс: React, TypeScript, Vite | [package.json](../frontend/package.json) и [bun.lock](../frontend/bun.lock); Bun 1.4.2 |
| Go-сервисы | `go.mod` и `go.sum` в каждом `backend/<сервис>`; список модулей — `SERVICES` в [Makefile](../Makefile), общая библиотека — `backend/platform` |
| Обучение модели | [requirements.txt](../scripts/weak-signal/requirements.txt): NumPy 2.5.3, scikit-learn 1.9.1 |
| Python-сервисы моделей | `requirements.txt` в соответствующих папках [ml-services](../ml-services); у необязательного `diagram-parser-service` — `pyproject.toml` и `uv.lock` |
| Rust-компоненты | `Cargo.toml` и `Cargo.lock` в [archive-scan](../backend/archive-scan), [graph-compute](../ml-services/graph-compute) и [workbook-parser-service](../ml-services/workbook-parser-service) |

Образы вспомогательных сервисов и их версии перечислены в [Docker Compose](../infra/docker-compose.yml),
базовые образы собственных сервисов — в их Dockerfile. Подключение моделей и ключей —
в [руководстве по развёртыванию](deployment_guide.pdf).
