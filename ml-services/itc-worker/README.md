# itc-worker

Пересчитывает индекс технологичности (ITC) тем и их связи с гипотезами по расписанию и по
`POST /admin/itc/run`.

## Как работает

1. Первый запуск — через `ITC_STARTUP_DELAY_SEC` (при `ITC_RUN_ON_START=false` — через
   `ITC_INTERVAL_SEC`). Каждые `ITC_CHECK_INTERVAL_SEC` воркер проверяет ключ
   `rag:itc:trigger`: его ставят `POST /admin/itc/run` и `cluster-worker` после новой доски.
2. Расчёт — `itc.py` отдельным процессом с лимитом `ITC_TIMEOUT_SEC`. Он читает через API
   `/clusters?view=full`, `/hypotheses`, `/hypotheses/graph`, `/documents` и первые фрагменты
   представительных документов тем. Плановый прогон сверяет подпись входов (состав тем, даты
   документов, цитируемость, эпоха корпуса, рубрика, текущая дата) с `rag:itc:input_signature`:
   при совпадении индекс тем не пересчитывается и фрагменты не читаются, связи гипотез
   проверяются как обычно. Запрос через `rag:itc:trigger` считает всё заново.
3. Четыре компонента на тему: научный моментум (годы публикаций), новизна (удалённость
   центроида темы от центра корпуса по эмбеддингам `EMB_URL`), импакт (цитируемость в графе
   доказательств), охват (документы и организации). Каждый — перцентильный ранг среди тем
   корпуса; TechScore = 0,40·SM + 0,25·NV + 0,20·IP + 0,15·HR, полоса 1–10 по рубрике.
4. Для связи гипотезы с темой нужна хотя бы одна непустая цитата `supports`/`contradicts`.
   Все процитированные документы, включая `context`, но исключая исходный запрос
   (`origin=input`), должны входить в `params.members` ровно одной темы; иначе связь
   не назначается. API повторяет проверку на текущей публикации и сохраняет индекс темы с
   `scope=hypothesis(document_membership)` и документами основания. Это не собственный
   индекс гипотезы. Прежние неподтверждённые значения массово не очищаются.
   Если карточка отсутствует или недоступна, API отклонил членство документов либо
   сообщил конфликт версии при записи связи, пропускается только эта связь.
   Остальные ошибки останавливают расчёт.
   Запись — `POST /clusters/{id}/itc` и
   `POST /hypotheses/{id}/itc`; статус — в Valkey (`rag:itc:last_status`,
   `rag:worker:itc:status` для `GET /system/activity`).

Расчёт детерминирован. Если у темы с документами нет состава (`members`/`representatives`),
расчёт останавливается с ошибкой — сначала перестройте темы. При ошибке прежние значения
остаются.

ITC — библиометрический индекс относительно корпуса, а не уровень готовности технологии
(TRL) и не оценка модели.

## Настройки

Адреса и учётная запись (`VALKEY_ADDR`, `RAG_GW`, `RAG_USER`, `RAG_PASS`, `EMB_URL`) задаёт
Compose. Неочевидное:

- `ITC_REPS` — сколько представительных фрагментов темы идёт в центроид;
- `ITC_DRY` — считать без записи в API;
- `ITC_RUBRIC_PATH` — рубрика. Локальная [itc_rubric.json](itc_rubric.json) — копия
  [backend/main-service/internal/application/itc_rubric.json](../../backend/main-service/internal/application/itc_rubric.json),
  которую отдаёт `GET /itc/rubric`; менять обе вместе.

## Проверка

Из `ml-services/itc-worker`:

```sh
docker compose -f ../../infra/docker-compose.yml logs --tail=120 itc-worker
RAG_GW=http://localhost:18080 RAG_PASS=... ITC_DRY=1 PYTHONPATH=../runtime python3 itc.py
PYTHONPATH=../runtime python -m unittest -v test_itc.py
```
