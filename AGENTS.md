# Рабочий контракт репозитория

Для любого, кто садится за этот код, — человека или агента: как не сломать чужое и пройти
проверки. Остальная документация — в разделе «Документация» [README](README.md#документация).

## Карта репозитория

| Путь | Что там |
| --- | --- |
| `backend/platform` | общая Go-библиотека (`pkg`): конфиг, логгер, gRPC, хранилища, `genproto` |
| `backend/<сервис>` | 14 самостоятельных Go-модулей, у каждого свой `go.mod`; список — `SERVICES` в [Makefile](Makefile) |
| `backend/trend-sources` | сборщики источников радара: один образ, вид сборщика задаёт `SOURCE_KIND` |
| `backend/main-service/cmd` | точки входа: API (`server`), `radar-collector`, `radar-analysis` и служебные утилиты |
| `backend/proto` | контракты gRPC (`buf`), стабы генерирует `make proto` |
| `backend/archive-scan` | Rust-утилита разбора архивов, вне Go-целей Makefile |
| `frontend/` | SPA на React 19 + Vite + Tailwind 4, пакетный менеджер `bun` |
| `ml-services/` | Python- и Rust-сервисы и воркеры: эмбеддинги, реранкер, распознавание, темы, сигналы |
| `infra/` | `docker-compose.yml` и оверлеи, nginx, Prometheus/Grafana, `trend-sources.json`; секреты в `infra/.env` (в Git не входит) |
| `docs/` | руководства, `ARCHITECTURE.md`, обзорная схема (`docs/architecture.png`) и презентация (`docs/presentation.pdf`) |
| `scripts/` | `vendor-platform.sh`, `gen-proto.sh`, `radar-acceptance.py`, обучение модели этапа 1 (`weak-signal/`), гейт качества (`quality/`), `tools/` |
| `datasets/`, `models/` | пример файла для проверки модели; веса моделей скачиваются при первом старте, в Git не входят |

## Зоны ответственности

Репозиторий ведут параллельно. Правило одно: не коммитить в чужую зону, даже если правка
кажется очевидной.

| Зона | Кто ведёт |
| --- | --- |
| `frontend/`, `backend/*/internal/interfaces`, `backend/db-service`, парсеры и конвейер документов, UX, производительность | наша |
| `backend/main-service/internal/application/trend_live_*`, `study_*`, `backend/trend-sources` — контур слабых сигналов | наша |
| Остальное в `backend/*/internal/application`, `backend/llm-service`, `ml-services/` | ML-коллега — не коммитить |

Перед правкой — `git status --short` и `git rev-parse --abbrev-ref HEAD`: в одном чекауте может
работать другая сессия. Смешанный коммит из двух зон разбирать потом некому.

## Сборка и проверки

Команды запускаются из корня; проверены на macOS с Go 1.27, bun 1.4.2 и golangci-lint 2.14.

| Команда | Что делает |
| --- | --- |
| `make help` | список целей Makefile |
| `make build` | `go build ./...` в платформе и каждом сервисе |
| `make test` | `go test ./...` во всех модулях; на прогретом кэше около 65 с |
| `make lint` | `golangci-lint run ./...` по модулям последовательно (линтер держит глобальный лок) |
| `make vet`, `make fmt`, `make tidy` | `go vet`, `gofmt -w`, `go mod tidy` по всем модулям |
| `make demo-resilience` | три юнита мягкой деградации RAG-ответа |
| `make dev` | бэкенд в контейнерах, интерфейс — локальный Vite |
| `cd frontend && bun run verify` | полный фронтовый гейт: `fmt:check` → `lint` → `typecheck` → `knip` → `audit` → `test` → `build` → `size` |
| `cd frontend && bun run verify:ui` | то же плюс браузерный набор UI kit (`playwright.ui-kit.config.ts`) |
| `make quality-unit` | гейт выпуска без внешних хранилищ: Go, фронт, Python, Rust; нужны Go, bun, uv, Rust, `pkg-config` и libarchive |
| `make quality-integration` | обязательные тесты на выделенных хранилищах из `*_TEST_*` (`DB_TEST_DSN` и др.); адреса рабочего контура не передавать — тесты пишут в хранилища |
| `make hooks` | поставить git-хуки из [lefthook.yml](lefthook.yml) |

`make frontend-check` делает то же, что `bun run verify`, но сначала может запустить
`bun install --frozen-lockfile`: цель зависит от файла-метки `frontend/node_modules/.install.stamp`,
которого после ручной установки нет.

Повторяющиеся последовательности вынесены в навыки `.claude/skills/`:
[verify-before-commit](.claude/skills/verify-before-commit/SKILL.md) — проверки перед коммитом,
[radar-acceptance](.claude/skills/radar-acceptance/SKILL.md) — приёмка живого поиска перед
показом.

## Стиль

- Комментарий объясняет **почему**, а не что делает строка. Пишется по-русски; русский и
  английский в одном комментарии не смешивать. Образец — заголовки
  `backend/main-service/internal/application/trend_live_confidence.go` и
  `frontend/src/shared/design-system/tokens.css`.
- Линт чистый без `//nolint`. Конфигурация — [.golangci.yml](.golangci.yml); каждое новое
  подавление нужно защищать.
- Тесты не плодить. Бюджет всех тестов — около трёх минут; один проверяющий тест на нетривиальную
  логику лучше, чем набор на каждую функцию.
- Форматтер фронтенда — `oxfmt` (`bun run fmt`), не prettier. Он форматирует и корневой
  `DESIGN.md`; чтобы не задеть чужие файлы, после правки форматируйте адресно из корня:
  `./frontend/node_modules/.bin/oxfmt --config frontend/.oxfmtrc.json DESIGN.md`.
- Границы фронтенда проверяются кодом, а не ревью: `shared` не импортирует `features`, между
  фичами — только публичный модуль (`frontend/scripts/check-conventions.ts`), структурные
  правила UI — в `ast-grep` (`frontend/sgconfig.yml`), покрытие токенов и каталога —
  `lint:design`.
- Русские тексты интерфейса проверяет `frontend/scripts/check-copy.ts`: латиница только из белого
  списка, неразрывный пробел перед `%`, тире, кавычки и многоточие — типографские.

## Грабли

| Что | Правило |
| --- | --- |
| `make vendor` | `backend/*/internal/platform` — вендоренные копии `backend/platform/pkg`; скрипт удаляет каталог целиком и копирует заново, прямые правки в нём теряются. Править источник, потом `make vendor` |
| Фронтовые юниты | `bun run test` (это `test:ast` + `bun test src scripts`). Голый `bun test` подхватывает ещё и playwright-спеки из `frontend/tests/` и падает |
| `make lint` | останавливается на первом модуле с ошибкой, остальные не проверяются. Прогонять до конца после починки |
| `make proto` | требует `protoc` и `grpcio-tools`; после генерации обязателен `make vendor` |
| Живой стенд | только точечная выкладка `make deploy SERVICES="…"`, `make up` там не запускать |

## Ветки

`develop` — рабочая, `master` — последняя готовая версия. Мержить develop в свой worktree, когда
он свободен, и сверять `git worktree list`. Ничего не коммитить и не пушить без явной просьбы.
