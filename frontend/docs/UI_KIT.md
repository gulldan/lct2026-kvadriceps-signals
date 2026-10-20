# UI kit

Запуск: `bun run ui-kit`. Адрес: `/ui-kit.html`. Это отдельный entry Vite без API, пользовательской сессии и production-данных. Изменения в `shared/ui` сразу обновляют приложение и каталог. Для проверки собранной версии: `bun run build && bun run preview`, затем `/ui-kit.html`.

`#trend-fixtures` показывает 26 синтетических записей источников с эталонной разметкой. Исходник — `backend/platform/pkg/sourcedoc/testdata/v1/corpus.json`; копия в `tests/fixtures/signalgraph-corpus-v1.json` нужна, чтобы фронтенд собирался в Docker самостоятельно. После правки исходника обновите копию до побайтного совпадения — его проверяет `scripts/tools/data/test_signalgraph_foundation.py`. Продуктовая сборка этот корпус не импортирует.

## Композиция рабочих страниц

- `PageLayout` задаёт общую ширину 1280 px и адаптивные отступы, в том числе для поиска и отчётов на экране.
- `PageHeader` / `PageTitle` задают единый заголовок, `PageToolbar` / `BackButton` — навигацию и действия над страницей. Основные действия в шапке имеют стандартный размер.
- `FilterBar` объединяет поиск и фильтры: `Input` / `SearchField`, `SelectTrigger`, `Button` и `Segmented` наследуют высоту ряда. По умолчанию размер стандартный, `size="sm"` делает всю панель компактной; вне панели собственные размеры контролов сохраняются. Примеры обоих размеров — `#filter-bars`.
- `ReadingContent` ограничивает длину строки и задаёт текст 16 px / 28 px; `Section reading` и `Disclosure reading` оформляют главы и подробные пояснения. Методика, пример радара и фрагменты источника используют эту композицию.
- `MetricTile variant="plain"` — сводка без отдельной карточки; подписи, числа и пояснения выравниваются общей сеткой.
- `Section`, `SettingsCard` и `Panel` используют плоское разделение линиями. `Disclosure` — раскрываемые дополнительные параметры с нативным управлением клавиатурой.
- `BarChart` принимает `{ label, value, displayValue }[]`: равные интервалы, нулевые значения, точные подписи по наведению и фокусу.
- `DonutChart` показывает взаимоисключающие доли: `{ key, label, value, color }[]`, где `color` — токен палитры. Значения и проценты доступны в легенде, по фокусу, наведению и касанию. Неклассифицированные данные передаются отдельной категорией; нули не рисуются. Для длинной легенды группируйте малые доли и сохраняйте их полный состав рядом с диаграммой.
- `TimeSeriesChart` показывает измеренные ряды и компактные мини-графики на общей шкале; пропуски остаются разрывами. API описан ниже, живой пример — в разделе «Композиция» (`#surfaces`).
- `DropdownMenuContent semantics="dialog"` использует нативный `dialog` для уведомлений с текстом и действиями. Обычные списки команд сохраняют семантику меню.
- `ScrollArea` — общая нативная прокрутка для таблиц, отчётов и боковых панелей. Обязательный `aria-label` именует область; Tab переводит в неё фокус, стрелки/Home/End прокручивают содержимое. Кнопка закрытия Sheet остаётся вне прокручиваемого тела. Единственное исключение правила `no-noninteractive-tabindex` живёт здесь: так прокрутка доступна с клавиатуры ([MDN](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/overflow#accessibility), [правило](https://github.com/jsx-eslint/eslint-plugin-jsx-a11y/blob/main/docs/rules/no-noninteractive-tabindex.md)).

## Структура

| Путь                                  | Ответственность                                                         |
| ------------------------------------- | ----------------------------------------------------------------------- |
| `src/shared/design-system/tokens.css` | Цвета, алиасы, размеры контролов, радиусы, шрифты, тени, движение, слои |
| `src/shared/design-system/tones.ts`   | Готовые цветовые схемы бейджей и категорий диаграмм                     |
| `src/shared/design-system/colors.ts`  | Категориальные CSS-ссылки, чтение Canvas-токенов, печатная палитра      |
| `src/shared/ui`                       | Компоненты, доступность и варианты оформления                           |
| `src/shared/lib/cn.ts`                | Объединение классов с учётом собственных размеров Tailwind              |
| `src/ui-kit`                          | Живые примеры, документация использования и демонстрационные состояния  |
| `scripts/check-design-system.ts`      | Проверка токенов, использования компонентов и покрытия каталога         |
| `tests/ui-kit`                        | Проверки браузера без backend                                           |

## Использование

Импортируйте нужный модуль напрямую; `shared/ui/index.ts` оставлен для старых потребителей и не является отдельной копией библиотеки. `RichTextInner`, `PdfViewerInner` и `useOverlayFocus` — внутренние реализации, их не нужно импортировать из feature.

```tsx
import { Button } from "@/shared/ui/Button";
import { Badge } from "@/shared/ui/Badge";
import { FieldRow } from "@/shared/ui/Fields";
import { Input } from "@/shared/ui/Input";
import { Tabs, TabPanel } from "@/shared/ui/Tabs";

<Button onClick={create}>Создать гипотезу</Button>
<Badge variant="ok">Подтверждено</Badge>
<FieldRow id="title" label="Название">
  <Input id="title" required />
</FieldRow>
<Tabs id="hypothesis" tabs={tabs} active={active} onChange={setActive} />
<TabPanel tabsId="hypothesis" active={active}>{content}</TabPanel>
```

`Button` имеет варианты `default`, `destructive`, `outline`, `secondary`, `accent`, `ghost`, `link`; размеры `default`, `sm`, `lg`, `icon`, `icon-sm`, `inline`, `content`, `row`. Единственный основной вариант — `default`. Стандартная высота — 40 px, компактная — 32 px, крупная — 48 px; длинная подпись переносится, увеличивая высоту. `row` — полноширинная строка списка с общими отступами. `inline` — действие внутри текста, `content` — кнопка со свободной высотой и переносом подписи. В форме отправка всегда явная: `type="submit"`. `asChild` передаёт оформление единственному дочернему элементу, например ссылке.

`outline` с `aria-pressed="true"` или `aria-expanded="true"` получает цветное выделение без изменения размеров и толщины рамки. Одиночный фильтр выключается повторным нажатием, без дополнительного сброса. Действия в `DialogFooter` и других группах формы используют одинаковый размер; соседство основной и второстепенной кнопки проверяется и в открытых формах браузерных сценариев.

`Badge` имеет варианты `default`, `secondary`, `destructive`, `outline`, `muted`, `brand`, `ok`, `warn`, `risk`. Предметная логика выбирает вариант или готовый `badgeTones`, а не собирает классы цвета.

`Input`, `Textarea` и `SelectTrigger` используют общий `inputStyles`; рамка, фон, радиус, focus, disabled и `aria-invalid` согласованы. `Input controlSize` и `SelectTrigger size` выбирают размер из тех же токенов, что и кнопки. `SearchField` использует `Input`, а `SearchBar` поиска компонует это поле с обычной кнопкой отправки. `Segmented` также читает общие токены высоты. Сообщения ошибок и подсказки связываются с конкретным контролом через `aria-describedby`. `FieldRow` связывает label по `id`; hint выводится под контролом, а его доступное описание задаётся на контроле как `aria-describedby="<id>-hint"`. Длинное пояснение не смещает соседнее поле. `ToggleRow` связывает собственную подсказку автоматически.

`FieldRow layout="row"` используется в настройках: слева подпись с необязательным пояснением, справа контрол; на телефоне поле переносится под подпись. Размер области ввода и отступы общие для модели, тарифов, лимитов и весов. Служебные параметры раскрываются через `Disclosure`; при ошибке в закрытой группе форма сначала раскрывает её для нативной валидации и фокуса.

`FieldRow layout="compact"` скрывает только визуальную подпись в плотной панели поиска; связь label с полем и структура DOM сохраняются. Переключение из обычной формы в компактную не сбрасывает фокус и введённый текст. `StatStrip columns={2}` располагает парные измерения в двух колонках, в том числе на телефоне; по умолчанию колонок три.

`Card asChild` передаёт поверхность одному дочернему элементу (`article`, `nav`, `aside`, `output`), сохраняя его семантику без лишнего div. `Notice` показывает контекстное сообщение с тоном `brand`, `ok`, `warn` или `risk`; `role="alert"` задаётся только для новой срочной ошибки.

`Segmented` — радиогруппа, а не вкладки; варианты могут быть `disabled`. Для вкладок используйте пару `Tabs` / `TabPanel` с уникальным `id`. Панель контролируется родителем, а ID и aria-связи — UI kit. Если проверка формы или сохранение запрещает переход, верните `false` из `onChange`: клавиатурное переключение не перехватит фокус у поля с ошибкой.

В `DialogContent` и `SheetContent` всегда нужны title и description (либо явное доступное описание). Обёртки сохраняют стандартные callbacks Radix `onOpenAutoFocus` / `onCloseAutoFocus`; пользовательский `preventDefault()` остаётся приоритетным.

`PdfViewer` — общий ленивый просмотрщик: локальный worker, масштаб и переход к цитате. Передайте URL и при необходимости `highlight`. Файлы загружает feature; в UI kit доступен локальный пример. `toast` импортируется из `@/shared/ui/Sonner`, типы Markdown — из `@/shared/ui/RichText`.

## Как развивать

1. Найдите ближайший образец в каталоге и реализацию в `shared/ui`.
2. Цвета меняйте в `palette-*` в `tokens.css`; остальные роли — их алиасы; новый статус добавляйте в `tones.ts`. Не вводите вторую таблицу значений в TS.
3. Добавляйте новый вариант в существующий компонент либо переносите повторяющийся UI в общий слой вместе со всеми его потребителями. Не создавайте слой-обёртку без собственной ответственности.
4. Добавьте живой пример в соответствующий раздел `src/ui-kit` и обновите реестр ниже. Покажите нестандартные состояния: disabled, invalid, empty, loading.
5. Для нового семантического размера обновите `cn.ts` и его проверку слияния.
6. Запустите `bun run verify:ui`. При новом поведении расширяйте существующие browser-тесты.

Гейт запускает отдельный Vite из текущего checkout. Если порт 4174 занят другой сессией,
задайте свободный: `UI_TEST_PORT=4182 bun run verify:ui`. Уже работающий сервер не переиспользуется,
чтобы тесты случайно не проверили код соседнего worktree.

## Реестр компонентов

| Модуль                                                    | Публичные компоненты / API                                                                                                                                                 |
| --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [AiNote](../src/shared/ui/AiNote.tsx)                     | `AiNote`                                                                                                                                                                   |
| [Avatar](../src/shared/ui/Avatar.tsx)                     | `Avatar`, `AvatarFallback`                                                                                                                                                 |
| [Badge](../src/shared/ui/Badge.tsx)                       | `Badge`                                                                                                                                                                    |
| [BarChart](../src/shared/ui/BarChart.tsx)                 | `BarChart`, `BarDatum`                                                                                                                                                     |
| [DonutChart](../src/shared/ui/DonutChart.tsx)             | `DonutChart`, `DonutDatum`                                                                                                                                                 |
| [Button](../src/shared/ui/Button.tsx)                     | `Button`                                                                                                                                                                   |
| [Card](../src/shared/ui/Card.tsx)                         | `Card`, `CardButton`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`                                                                                          |
| [CardGridSkeleton](../src/shared/ui/CardGridSkeleton.tsx) | `CardGridSkeleton`                                                                                                                                                         |
| [Chip](../src/shared/ui/Chip.tsx)                         | `Chip`                                                                                                                                                                     |
| [CounterStrip](../src/shared/ui/CounterStrip.tsx)         | `CounterStrip`, `CounterItem`                                                                                                                                              |
| [CSwitch](../src/shared/ui/CSwitch.tsx)                   | `Switch`                                                                                                                                                                   |
| [DateStrip](../src/shared/ui/DateStrip.tsx)               | `DateStrip`, `DateStripItem`                                                                                                                                               |
| [Dialog](../src/shared/ui/Dialog.tsx)                     | `Dialog`, `DialogContent`, `DialogHeader`, `DialogFooter`, `DialogTitle`, `DialogDescription`                                                                              |
| [Disclosure](../src/shared/ui/Disclosure.tsx)             | `Disclosure`                                                                                                                                                               |
| [DropdownMenu](../src/shared/ui/DropdownMenu.tsx)         | `DropdownMenu`, `DropdownMenuTrigger`, `DropdownMenuContent`, `DropdownMenuItem`, `DropdownMenuCheckboxItem`, `DropdownMenuLabel`, `DropdownMenuSeparator`                 |
| [EmptyState](../src/shared/ui/EmptyState.tsx)             | `EmptyState`                                                                                                                                                               |
| [ErrorState](../src/shared/ui/ErrorState.tsx)             | `ErrorState`                                                                                                                                                               |
| [EvidenceTally](../src/shared/ui/EvidenceTally.tsx)       | `EvidenceTally`                                                                                                                                                            |
| [Fields](../src/shared/ui/Fields.tsx)                     | `ToggleRow`, `FieldRow`, `StatStrip`                                                                                                                                       |
| [FileTypeBadge](../src/shared/ui/FileTypeBadge.tsx)       | `FileTypeBadge`                                                                                                                                                            |
| [InfoHint](../src/shared/ui/InfoHint.tsx)                 | `InfoHint`                                                                                                                                                                 |
| [Input](../src/shared/ui/Input.tsx)                       | `inputStyles`, `Input`                                                                                                                                                     |
| [Kicker](../src/shared/ui/Kicker.tsx)                     | `Kicker`                                                                                                                                                                   |
| [Label](../src/shared/ui/Label.tsx)                       | `Label`                                                                                                                                                                    |
| [LanguageSwitcher](../src/shared/ui/LanguageSwitcher.tsx) | `LanguageSwitcher`                                                                                                                                                         |
| [LegendItem](../src/shared/ui/LegendItem.tsx)             | `LegendItem` — маркер и подпись строки легенды или стадии: заливка токеном или `color`, контур `dashed`/`solid`, размер `xs`/`sm`/`md`, форма круг/квадрат                 |
| [ListSkeleton](../src/shared/ui/ListSkeleton.tsx)         | `ListSkeleton`                                                                                                                                                             |
| [MetricTabs](../src/shared/ui/MetricTabs.tsx)             | `MetricTabs`, `MetricTabItem`                                                                                                                                              |
| [MetricTile](../src/shared/ui/MetricTile.tsx)             | `MetricTile`                                                                                                                                                               |
| [Notice](../src/shared/ui/Notice.tsx)                     | `Notice`                                                                                                                                                                   |
| [PageHeader](../src/shared/ui/PageHeader.tsx)             | `PageHeader`, `PageTitle`, `PageToolbar`, `BackButton`                                                                                                                     |
| [PageLayout](../src/shared/ui/PageLayout.tsx)             | `PageLayout`                                                                                                                                                               |
| [Pagination](../src/shared/ui/Pagination.tsx)             | `Pagination`, `pageItems`                                                                                                                                                  |
| [Panel](../src/shared/ui/Panel.tsx)                       | `Panel`, `FieldLabel`, `Field`, `BulletList`                                                                                                                               |
| [PdfViewer](../src/shared/ui/PdfViewer.tsx)               | `PdfViewer`, `PdfHighlight`                                                                                                                                                |
| [Range](../src/shared/ui/Range.tsx)                       | `Range`                                                                                                                                                                    |
| [ReadingContent](../src/shared/ui/ReadingContent.tsx)     | `ReadingContent`                                                                                                                                                           |
| [ResultCard](../src/shared/ui/ResultCard.tsx)             | `ResultCard`, `ResultGrid`, `ResultTile`, `ResultTileMedia`, `ResultMeta`, `ResultItem`                                                                                    |
| [RichText](../src/shared/ui/RichText.tsx)                 | `RichText`                                                                                                                                                                 |
| [ScrollArea](../src/shared/ui/ScrollArea.tsx)             | `ScrollArea` — именованная область с нативной клавиатурной прокруткой                                                                                                      |
| [SearchField](../src/shared/ui/SearchField.tsx)           | `SearchField`                                                                                                                                                              |
| [Section](../src/shared/ui/Section.tsx)                   | `Section`                                                                                                                                                                  |
| [SegmentBar](../src/shared/ui/SegmentBar.tsx)             | `SegmentBar`                                                                                                                                                               |
| [Segmented](../src/shared/ui/Segmented.tsx)               | `Segmented`                                                                                                                                                                |
| [Select](../src/shared/ui/Select.tsx)                     | `Select`, `SelectValue`, `SelectTrigger`, `SelectContent`, `SelectItem`                                                                                                    |
| [Separator](../src/shared/ui/Separator.tsx)               | `Separator`                                                                                                                                                                |
| [SettingsCard](../src/shared/ui/SettingsCard.tsx)         | `SettingsCard`                                                                                                                                                             |
| [Sheet](../src/shared/ui/Sheet.tsx)                       | `Sheet`, `SheetContent`, `SheetHeader`, `SheetTitle`, `SheetDescription`                                                                                                   |
| [SignalDial](../src/shared/ui/SignalDial.tsx)             | `SignalDial`, `DialRay`, `DIAL`                                                                                                                                            |
| [Skeleton](../src/shared/ui/Skeleton.tsx)                 | `Skeleton`                                                                                                                                                                 |
| [Sonner](../src/shared/ui/Sonner.tsx)                     | `Toaster`, `toast`                                                                                                                                                         |
| [Spinner](../src/shared/ui/Spinner.tsx)                   | `Spinner`                                                                                                                                                                  |
| [StandaloneLayout](../src/shared/ui/StandaloneLayout.tsx) | `StandaloneLayout` — оболочка служебной страницы без сессии; `preview` убирает вложенный `main` в каталоге.                                                                |
| [StatusPage](../src/shared/ui/StatusPage.tsx)             | `StatusPage` — состояния 403, 404, 500 и 503, действия `onRetry` / `onBack`, `titleAs` для вложенного экрана.                                                              |
| [Table](../src/shared/ui/Table.tsx)                       | `Table`, `TableHeader`, `TableBody`, `TableRow`, `TableHead`, `TableCell`                                                                                                  |
| [Tabs](../src/shared/ui/Tabs.tsx)                         | `Tabs`, `TabPanel`                                                                                                                                                         |
| [Textarea](../src/shared/ui/Textarea.tsx)                 | `Textarea`                                                                                                                                                                 |
| [ThemeSwitcher](../src/shared/ui/ThemeSwitcher.tsx)       | `ThemeSwitcher`                                                                                                                                                            |
| [TimeSeriesChart](../src/shared/ui/TimeSeriesChart.tsx)   | `TimeSeriesChart`, `TimeSeries`                                                                                                                                            |
| [Tooltip](../src/shared/ui/Tooltip.tsx)                   | `TooltipProvider`, `Tooltip`, `TooltipTrigger`, `TooltipContent`                                                                                                           |
| [ValueScale](../src/shared/ui/ValueScale.tsx)             | `ValueScale` — нативный `meter`: измеренное `value` от 0 до 100, доступная подпись `label`, отметка середины шкалы. Отсутствующее измерение передаётся текстом, без шкалы. |

## Временные ряды

`TimeSeriesChart` принимает обязательные `series: TimeSeries[]` и доступную подпись `label: string`. Каждый `TimeSeries` содержит `id`, `label` и `points: { period: string; value: number | null; display: string }[]`. Передавайте неотрицательные измерения и одинаковые периоды в одинаковом порядке во всех рядах: горизонтальные интервалы равны, их подписи берутся из первого ряда. Прямые отрезки соединяют соседние измерения; `null` разрывает линию, а ноль рисуется на нулевой отметке. Точная подпись использует готовый `display`, включая локализованное обозначение отсутствующих данных.

| Параметр                                  | Поведение                                                                                                                                                |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `maximum?: number`                        | Верхняя граница шкалы. Без неё вычисляется максимум всех рядов, не меньше 1. Для сопоставимых графиков передавайте одну границу, вмещающую все значения. |
| `compact?: boolean`                       | По умолчанию `false`. Компактный вариант — подписанный SVG без осей и управления периодами; подробное доступное описание передаётся через `label`.       |
| `selected?: string`                       | Выбранный период; состояние хранит вызывающий компонент.                                                                                                 |
| `onSelect?: (period: string) => void`     | Вызывается при нажатии на график или активации кнопки периода с клавиатуры. Снятие выбора и фильтрацию реализует вызывающий компонент.                   |
| `formatValue?: (value: number) => string` | Форматирует отметки вертикальной оси; по умолчанию `String`.                                                                                             |

В полном варианте подпись показывает значения периода под указателем, затем выбранного периода, а без выбора — последнего. Кнопки периодов имеют `aria-pressed`, видимый фокус и стандартную компактную высоту. Именованный `ScrollArea` удерживает оси, график и кнопки внутри своей области на узком экране и при увеличении текста; координаты наблюдений не меняются. Ряды последовательно используют алиасы `brand`, `chart-2`, `chart-3`; после третьего цвет повторяется, поэтому различайте ряды подписями. Расчёты долей, сравниваемые семейства, научные пороги и сохранение состояния в URL принадлежат feature.

## Контроль качества

`tests/product/pages.spec.ts` открывает основные маршруты на ширинах 390 и 1440 px, а также вход и отказ в доступе. На каждой странице проверяются общие заголовки и границы контента, фон и высоты контролов; изменение токенов `--primary` и `--control-md` обязано обновить все соответствующие элементы. Дополнительно сохраняются снимки, проверяются горизонтальное переполнение и axe.

`tests/product` проверяет реальные страницы на изолированных данных: действия меняют состояние API-фикстуры, а тесты проверяют экран, отправленный запрос и результат повторной загрузки. Проверки без бэкенда не заменяют сценарии с API: `e2e` и `a11y` по живому стенду остаются отдельными командами.

Браузерный набор каталога проверяет основные цвета палитры, загрузку кириллицы Onest и JetBrains Mono, пары контраста, axe, ширины 390/768/1024/1440 px, клавиатуру, radio/select/menu, восстановление фокуса, все стороны Sheet, пагинацию, ошибки, размеры через токены и reduced motion. `layout.spec.ts` измеряет выравнивание полей с бейджами и длинными подсказками, строки палитры, общие колонки карточек и метрик, расстояние между переключателями и отсутствие переполнения раскрытого справочника. Отдельно проверяются PDF/worker/масштаб, формулы/mhchem и форма входа. PNG и trace сохраняются; автоматического сравнения PNG с эталоном нет.

`lint:ast` запускает пять правил из `ast-grep/rules`: контролы и таблицы только через UI kit, UI-библиотеки только в общем слое, никаких копий Card, даты и размеры через общий форматтер. Правила разбирают AST, включая динамические импорты, и не срабатывают на комментарии. `test:ast` проверяет допустимые и ошибочные примеры каждого правила.

`lint:design` проверяет литеральные цвета, стандартную палитру Tailwind, неопределённые CSS-переменные, циклы алиасов и наличие живого примера каждого публичного модуля в `src/ui-kit`. В feature/app запрещены локальные оттенки через прозрачность, нативный `h1`, переопределения цвета, типографики и числовой высоты общих контролов; условные JSX-классы тоже проверяются. Геометрия диаграмм берётся из данных, а цветовые рецепты — из `chartTones`. `check-conventions.ts` проверяет направление зависимостей, включая re-export и lazy import.

Oxfmt форматирует TS/TSX/CSS/JSON/Markdown/YAML и сортирует Tailwind с реальным `index.css`. Oxlint работает с `--deny-warnings`. Knip анализирует приложение, каталог, scripts, тесты и CSS без списков игнорируемых зависимостей. Не объявляйте каждый исходник entry-файлом: это скрывает мёртвые экспорты.

Бюджеты заданы только в `.size-limit.ts`: по 260 KB Brotli на начальный JS приложения и каталога плюс пределы отдельных чанков. Markdown и PDF не должны попадать в начальную загрузку — это проверяется по манифесту Vite.

## Выводы по библиотекам

1. Нужные React/Radix/CVA/Tailwind-инструменты уже установлены. Новый UI framework и Storybook не нужны: второй HTML entry Vite (`/ui-kit.html`) показывает реальные компоненты без backend.
2. Tailwind 4 держит тему в общем CSS через `@theme inline`; палитра вынесена туда, стандартные цветовые классы отключены. [Документация](https://tailwindcss.com/docs/theme).
3. Пользовательские размеры зарегистрированы и в tailwind-merge, иначе `cn` может принять размер текста за цвет или не удалить конфликт высот; закреплено unit-тестом. [API](https://github.com/dcastil/tailwind-merge/blob/main/docs/api-reference.md).
4. Radix оставляет стили приложению и даёт управление фокусом. Для контролируемых Dialog/Sheet без Trigger добавлено восстановление исходного фокуса с сохранением пользовательских callbacks. [Styling](https://www.radix-ui.com/primitives/docs/guides/styling), [Dialog](https://www.radix-ui.com/primitives/docs/components/dialog).
5. Вкладки и фильтры различаются семантикой: Tabs связаны с TabPanel и поддерживают стрелки/Home/End, Segmented использует нативные radio. [WAI-ARIA Tabs](https://www.w3.org/WAI/ARIA/apg/patterns/tabs/).
6. Markdown, KaTeX, PDF и Word остаются за lazy/dynamic imports; eager UI barrel для них ухудшил бы начальную загрузку.
7. Oxfmt сортирует Tailwind-классы; отдельный Prettier и второй линтер не добавлены. [Oxfmt sorting](https://oxc.rs/docs/guide/usage/formatter/sorting).
8. Axe и Playwright уже установлены; независимый UI-набор с keyboard/state/responsive-проверками построен на них. [Accessibility testing](https://playwright.dev/docs/accessibility-testing).
9. Oxlint: шесть правил на базе React Compiler (`refs`, `set-state-in-effect`, `exhaustive-effect-dependencies`, `preserve-manual-memoization`, `immutability`, `no-deriving-state-in-effects`) выключены — их включение требует отдельной миграции управления состоянием; классический `react/exhaustive-deps` работает. [Правила](https://oxc.rs/docs/guide/usage/linter/rules/react/exhaustive-effect-dependencies.html).
10. Framer Motion не нужен: появления панелей используют CSS `motion-enter` / `motion-enter-side` с общими duration/easing и `prefers-reduced-motion`.

## Служебные страницы и длительный поиск

На `/login` форма собирается из `PageTitle`, `FieldRow`, `Input`, `Button`, `Notice` и `Disclosure`. На широком экране её сопровождает графический знак радара; на узком остаются бренд и форма. Знак декоративный и не изображает реальные данные. Справка о доступе раскрывается под формой. Размеры, цвета и состояния контролов наследуются из каталога, переключатели языка и темы общие с приложением.

`/403`, `/404`, `/500` и `/503` открываются без входа. Отказ живого поиска с кодом 503 использует тот же `StatusPage` внутри текущего маршрута: «Повторить» запрашивает результат для прежнего запроса. Смена страницы после сбоя отрисовки сбрасывает границу ошибки, обновление повторно загружает приложение.

В `StatusPage` код ошибки — крупный типографический рисунок в общей палитре. Ноль превращается в радар, закрытый сектор или паузу; при 500 один ноль распадается на три части. Появление проигрывается один раз, на 404 стрелка реагирует на мышь. Рисунок декоративный: состояние и действия всегда доступны текстом, движение не показывает ход восстановления. Уменьшение движения отключает анимацию и слежение. Геометрия, шрифты и CSS входят в компонент; отдельные картинки и библиотеки не нужны.

В каталоге `/ui-kit.html#search-progress` переключаются сбор, анализ, ожидание дольше восьми минут, потеря соединения и открытие сохранённого результата. Значения демонстрационные. В продукте этапы и находки приходят с сервера; секундомер привязывается к каждому полученному ответу. Этапы не считаются процентами времени. При потере связи сохраняется последний ответ, при смене запроса он скрывается. Пульс активного сегмента отключается при уменьшении движения.
