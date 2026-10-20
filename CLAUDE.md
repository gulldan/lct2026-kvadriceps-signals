# CLAUDE.md

Правила работы с репозиторием — в [AGENTS.md](AGENTS.md). Ниже только то, без чего ассистент
сделает неправильно.

- Не коммитить в чужую зону: `backend/llm-service`, `ml-services/` и `internal/application` вне
  контура слабых сигналов ведёт ML-коллега. Радар (`trend_live_*`, `study_*`, `trend-sources`) —
  наш. Перед правкой — `git status --short`: в чекауте может работать другая сессия.
- Ничего не коммитить и не пушить, пока об этом не попросили.
- Комментарии и документация — по-русски, объясняют «почему», а не «что»; русский и английский в
  одном комментарии не смешивать.
- Линт чистый без `//nolint`. Тесты не плодить: бюджет всех тестов — около трёх минут.
- Правки в `backend/*/internal/platform` затираются `make vendor` — это вендоренные копии
  `backend/platform/pkg`. Править источник.
- Фронтовый гейт — `cd frontend && bun run verify`; юниты — `bun run test`, не голый `bun test`.
  Форматтер `oxfmt`, не prettier.
- После правки `DESIGN.md` отформатировать его из корня:
  `./frontend/node_modules/.bin/oxfmt --config frontend/.oxfmtrc.json DESIGN.md` — pre-commit хук
  проверяет формат этого файла.
- На живом стенде не запускать `make up`: `env-bootstrap` рассинхронит пароль Postgres и контур
  ляжет. Только точечный `make deploy SERVICES="<сервисы>"`.
