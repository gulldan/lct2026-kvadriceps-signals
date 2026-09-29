# SignalGraph: итог приёмки

Приёмка качества **не пройдена**. Метод `signalgraph-method-v2-development-mmr` провалил
однократную оценку на holdout, вскрытом 2026-09-11 после фиксации кода и параметров. Этот
holdout раскрыт и для приёмки больше непригоден: следующая версия метода принимается только
по новой версии протокола на новом нетронутом holdout ([протокол](PROTOCOL.md)). Текущий
метод (`ml-services/signal-worker/method.json`) имеет `quality_status=not_frozen`; новой
отложенной оценки не было. Успешные тесты контрактов и регрессионные тесты подтверждают
реализацию, а не качество метода.

Разметку и повторную проверку делал один исполнитель (Codex) по первичным публикациям —
независимость оценки этим ограничена. Независимая экспертиза нужна для заявления
«проверено людьми», но не для запуска документальной оценки. `review_packet.py` готовит
слепые случаи development/validation с пустыми полями решения; сам пакет экспертизой не
является.

## Holdout v2

В контейнер ранжирования переданы код и входы без единого файла разметки; использованы
настоящие F2LLM и `graph-compute`, без публикации в БД и S3.

| Запрос | Система | Тем | Верных | P@15 | nDCG@15 | Разных gold-групп |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| AI | temporal+MMR | 5 | 2 | 0,133333 | 0,255914 | 2 |
| AI | keyword/count | 0 | 0 | 0 | 0 | 0 |
| RAG | temporal+MMR | 5 | 2 | 0,133333 | 0,255914 | 2 |
| RAG | keyword/count | 3 | 3 | 0,200000 | 0,363556 | 3 |
| Rare | temporal+MMR | 2 | 0 | 0 | 0 | 0 |
| Rare | keyword/count | 0 | 0 | 0 | 0 | 0 |

Gold-пулы (16 и 15) достаточны, но оба запроса провалили точность, nDCG, заполнение и
различие тем; RAG хуже независимого baseline. Редкий запрос честно сообщил
`insufficient_coverage`, но показал две ложные темы. Без MMR оценки те же; без временного
фильтра AI/RAG дают 6 тем, 3 верных, P@15 = 0,2. Утечек будущих ревизий и работ другого
разбиения во входе из 42 работ не найдено.

Обязательные поля девяти показанных карточек имеют точные цитаты, но одна из 27 ролей
неверна: у Speculative RAG фраза о результатах на ARC-Challenge стала одновременно
проблемой и преимуществом. Поэтому ворота «100 % обязательных полей» провалены.

Метрики, хеши входов, кода и моделей — [evaluations/20260911-holdout-v2.json](evaluations/20260911-holdout-v2.json).

## Development и validation

Входы разделены по каноническим работам и дате отсечения. Development — 64 работы и
29 размеченных тем, validation — 40 работ и 22 темы. Источники после просмотра ошибок не
дополнялись.

| Разбиение / запрос | Метод: тем | Метод: P@15 | Метод: nDCG@15 | Keyword/count: P@15 | Keyword/count: nDCG@15 |
| --- | ---: | ---: | ---: | ---: | ---: |
| Development / AI | 4 | 4/15 = 0,2667 | 0,4370 | 1/15 = 0,0667 | 0,1706 |
| Development / RAG | 4 | 3/15 = 0,2000 | 0,3636 | 2/15 = 0,1333 | 0,2783 |
| Validation / AI | 1 | 0/15 | 0 | 0/15 | 0 |
| Validation / RAG | 1 | 0/15 | 0 | 1/15 = 0,0667 | 0,1076 |

Во всех четырёх запросах провалены P@15, nDCG@15, заполнение и 15 разных gold-групп.
Пропуски сохраняют знаменатель 15, повторы и неизвестные группы положительной оценки не
получают.

Baseline `tfidf-keyword-count-v1` строит темы по TF-IDF слов и биграмм заголовков и
аннотаций, без разметки и семантических векторов: DF от 2 до половины корпуса, сходство с
исходным документом группы ≥ 0,25, минимум 2 работы, совпадение с запросом ≥ 0,6.
Обязательные поля извлекаются тем же кодом, что у основного метода.

## Почему пятнадцати тем нет

Даже с идеальными gold-группами временной фильтр пропускает лишь 10 из 17 тем AI и 9 из 15
тем RAG на development и 11 из 15 на validation: у остальных нет роста доли или свежей
научной поддержки. Значит, одной кластеризацией правильную пятнадцатку на этом корпусе не
получить. Вариант графа k=2, resolution=6, similarity=0,5 без mutual-kNN давал больше
карточек, но не больше верных тем и одну ложную на редком запросе — он не внедрён.

Остаются задачи: группировка, релевантность редкому запросу, полнота утверждений,
заранее собранный положительный срез не из ИТ, затухшие всплески и сопоставимая временная
популяция.

## Воспроизведение

Из корня репозитория, с зависимостями `signal-worker`. Экспорт и оценка используют только
выбранное разбиение; выходные файлы создаются эксклюзивно, поэтому для повтора нужна новая
папка.

```sh
python ml-services/eval-service/signalgraph/validate_benchmark.py --require-full-pool
python ml-services/eval-service/signalgraph/prepare_inputs.py --split development --destination /tmp/radar-review-inputs
python ml-services/eval-service/signalgraph/embed_inputs.py --input /tmp/radar-review-inputs/development.json --output /tmp/radar-review-inputs/development-vectors.json --url http://f2llm-service:8085/v1/embeddings
PYTHONPATH=ml-services/signal-worker:ml-services/runtime python ml-services/eval-service/signalgraph/run_ranking.py --sources /tmp/radar-review-inputs/development.json --vectors /tmp/radar-review-inputs/development-vectors.json --output /tmp/radar-review-inputs/development-run.json --graph graph-compute:9093
python ml-services/eval-service/signalgraph/assess_ranking.py --run /tmp/radar-review-inputs/development-run.json --output /tmp/radar-review-inputs/development-assessment.json
python ml-services/eval-service/signalgraph/review_packet.py --method ml-services/signal-worker/method.json --output /tmp/radar-review-inputs/independent-review.json
PYTHONPATH=ml-services/runtime python -m unittest discover -s ml-services/signal-worker/tests
python -m unittest discover -s ml-services/eval-service/signalgraph
go -C backend/platform test -race ./pkg/analysisdoc
go -C backend/main-service test -race ./internal/application -run Study
```

Для validation — те же команды с `validation`; `--development-config` допустим только для
development. Holdout требует явных `--freeze` и `--freeze-sha256`: `holdout_gate.py`
фиксирует код и параметры, а одноразовая квитанция запрещает второй прогон ранжирования под
тем же freeze. nDCG начисляется по протоколу: 3 — подходящая тема, 1 — подтверждённая
спорная или смежная, 0 — остальные.
