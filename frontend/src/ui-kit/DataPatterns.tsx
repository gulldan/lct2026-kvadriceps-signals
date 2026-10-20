import { PageLayout } from "@/shared/ui/PageLayout";
import { Disclosure } from "@/shared/ui/Disclosure";
import { BarChart } from "@/shared/ui/BarChart";
import { DonutChart } from "@/shared/ui/DonutChart";
import { TimeSeriesChart } from "@/shared/ui/TimeSeriesChart";
import { useState } from "react";
import { Database, FileText, SlidersHorizontal } from "lucide-react";

import { Avatar, AvatarFallback } from "@/shared/ui/Avatar";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import {
  Card,
  CardButton,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/shared/ui/Card";
import { EvidenceTally } from "@/shared/ui/EvidenceTally";
import { FileTypeBadge } from "@/shared/ui/FileTypeBadge";
import { LegendItem } from "@/shared/ui/LegendItem";
import { StatStrip } from "@/shared/ui/Fields";
import { CounterStrip } from "@/shared/ui/CounterStrip";
import { DateStrip } from "@/shared/ui/DateStrip";
import { MetricTabs } from "@/shared/ui/MetricTabs";
import { MetricTile } from "@/shared/ui/MetricTile";
import { TabPanel } from "@/shared/ui/Tabs";
import { Pagination } from "@/shared/ui/Pagination";
import {
  ResultCard,
  ResultGrid,
  ResultItem,
  ResultMeta,
  ResultTile,
  ResultTileMedia,
} from "@/shared/ui/ResultCard";
import { Section } from "@/shared/ui/Section";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { PageHeader, PageTitle, PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { SegmentBar } from "@/shared/ui/SegmentBar";
import { SignalDial, type DialRay } from "@/shared/ui/SignalDial";
import { ValueScale } from "@/shared/ui/ValueScale";
import { Separator } from "@/shared/ui/Separator";
import { SettingsCard } from "@/shared/ui/SettingsCard";
import { ScrollArea } from "@/shared/ui/ScrollArea";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import { AiNote } from "@/shared/ui/AiNote";
import { RichText } from "@/shared/ui/RichText";
import { PdfViewer } from "@/shared/ui/PdfViewer";
import demoPdf from "../../tests/fixtures/one-page.pdf?url";
import { SampleGroup, Specimen } from "./Specimen";
import { ReportExport } from "@/shared/report/ReportExport";
import { ReportBlocks } from "@/shared/report/ReportBlocks";
import { saveFile } from "@/shared/lib/download";
import type { ReportDoc } from "@/shared/report/blocks";
import { BulletList, Field, FieldLabel, Panel } from "@/shared/ui/Panel";

const SOURCE_TABS = [
  { id: "papers", label: "arXiv", value: "128" },
  { id: "repos", label: "GitHub", value: "31" },
  { id: "hn", label: "Hacker News", value: "0" },
  { id: "patents", label: "Патенты", value: "≥ 40", hint: "выборка" },
  { id: "web", label: "Сайты", value: "12" },
  { id: "registry", label: "Реестры", value: "—" },
  { id: "corpus", label: "Документы", value: "—" },
];

const DIAL_RAYS: DialRay[] = [
  {
    key: "semantic",
    label: "Отличие",
    value: "16,3 из 100",
    level: "Ближе всего: агенты",
    tone: "muted",
  },
  { key: "uniqueness", label: "Свои статьи", value: "92,7 %", level: "Высокая", tone: "ok" },
  { key: "impact", label: "Влияние", value: "0,2×", level: "Ниже области", tone: "warn" },
  { key: "relevance", label: "Релевантность", value: "—", level: "Нет данных", tone: "muted" },
  {
    key: "emergence",
    label: "Появление",
    value: "18 % · 30 %",
    level: "Латентная тема",
    tone: "muted",
  },
  { key: "novelty", label: "Новизна", value: "август 2025 г.", level: "Недавнее", tone: "ok" },
  { key: "momentum", label: "Динамика", value: "+0,028 п.п.", level: "Растёт", tone: "ok" },
  { key: "coherence", label: "Связность", value: "Статей: 470", level: "По запросу", tone: "warn" },
  { key: "spread", label: "Охват", value: "команд 23 · новых 16", level: "Широкое", tone: "ok" },
  {
    key: "application",
    label: "Применение",
    value: "репозиториев 8",
    level: "Кандидаты",
    tone: "muted",
  },
  { key: "concentration", label: "Концентрация", value: "62 %", level: "Высокая", tone: "warn" },
];

/** Пункты выгрузки живые и собирают настоящий файл, поэтому отчёт настоящий, но учебный. */
const DEMO_REPORT: ReportDoc = {
  fileName: "demo-report.docx",
  title: "Демонстрационный отчёт",
  blocks: [
    {
      kind: "doctitle",
      org: "Демонстрационные данные",
      doctype: "Отчёт по R&D-гипотезе",
      title: "Диагностика оборудования без остановки производства",
      meta: ["a1b2c3d4", "На проверке", "сформирован 23.09.2026"],
      lines: ["Экспертная верификация не проводилась"],
      facts: [
        { label: "Цель", value: "Надёжность оборудования (простои 12 → 8 ч/мес.)" },
        { label: "Тема", value: "Вибродиагностика насосов" },
      ],
      lead: "Плановые осмотры требуют доступа к оборудованию и не всегда позволяют заметить ранние признаки износа. Непрерывные измерения помогут определить, какие узлы проверить в первую очередь.",
      metrics: [
        { value: "72", unit: "/100", label: "Приоритет" },
        { value: "30", unit: "%", label: "Сокращение простоев" },
      ],
    },
    { kind: "heading", text: "1 Гипотеза и её обоснование" },
    {
      kind: "paragraph",
      text: "Датчики вибрации и температуры на группе насосов предупреждают об износе подшипников за две–три недели до отказа.",
    },
    {
      kind: "kv",
      rows: [
        { label: "Работ в окне", value: "128" },
        { label: "Вердикт проверки", value: "Скорее подтверждается (за 5, против 1)" },
      ],
    },
    {
      kind: "table",
      title: "Факторы приоритета",
      head: ["Фактор", "Вес", "Значение", "Вклад"],
      rows: [
        ["Соответствие цели", "0.30", "80%", "0.240"],
        ["Доказательства", "0.25", "64%", "0.160"],
      ],
    },
    {
      kind: "card",
      label: "1.1 — TRL 4 — На проверке",
      title: "Возврат тепла в технологический цикл",
      facts: [{ label: "Тема", value: "Теплообмен" }],
      lead: "Часть тепла процесса уходит в окружающую среду; его можно вернуть на предварительный нагрев потока.",
      about: "О гипотезе",
      text: "Проверить теплообменный модуль на одном участке установки и измерить энергопотребление до и после подключения.",
      metrics: [
        { value: "64", unit: "/100", label: "Приоритет" },
        { value: "4", unit: "/9", label: "Готовность (УГТ)" },
      ],
    },
    {
      kind: "list",
      items: ["Подтвердить точность предупреждений", "Оценить окупаемость"],
      ordered: true,
    },
    {
      kind: "quote",
      source: "[1] Отчёт об испытаниях, с. 12",
      text: "Рост амплитуды на частоте подшипника опережал отказ в среднем на 17 суток.",
    },
    {
      kind: "references",
      items: [{ n: 1, text: "Отчёт об испытаниях насосного парка — разделы 3–4" }],
    },
  ],
};

export function DataPatterns() {
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState(false);
  const [showPdf, setShowPdf] = useState(false);
  const [chartYear, setChartYear] = useState<string>();
  const [sourceTab, setSourceTab] = useState("papers");
  return (
    <>
      <Specimen
        id="surfaces"
        title="Карточки и композиция"
        description="Card задаёт поверхность; CardButton — действие всей карточки. SettingsCard и Section разделяют содержание линиями; MetricTile поддерживает сводку без фона. Образцы содержат демонстрационные данные."
        code={
          "<Card><CardHeader><CardTitle>Источники</CardTitle></CardHeader>\n  <CardContent>{children}</CardContent>\n</Card>\n<CardButton onClick={open}>Открыть гипотезу</CardButton>"
        }
      >
        <SampleGroup label="Временные ряды · демонстрационные значения, пропуск не равен нулю">
          <TimeSeriesChart
            label="Измерения по годам"
            selected={chartYear}
            onSelect={setChartYear}
            series={[
              {
                id: "example",
                label: "Пример",
                points: [
                  { period: "2022", value: 2, display: "2" },
                  { period: "2023", value: null, display: "нет данных" },
                  { period: "2024", value: 4, display: "4" },
                  { period: "2025", value: 7, display: "7" },
                ],
              },
            ]}
          />
        </SampleGroup>
        <SampleGroup label="Круговая сводка правил · демонстрационные значения">
          <div className="w-full">
            <SignalDial
              label="Профиль сигнала"
              caption="Кольца показывают, сколько признаков профиля подтверждено правилами: величины у строк разные, общей шкалы у них нет. Доля выполненных правил отбора считается отдельно — она в шапке карточки."
              centre="признаков"
              rings={["Без оценки", "Ниже планки", "Подтверждено"]}
              items={DIAL_RAYS}
            />
          </div>
        </SampleGroup>
        <ReadingContent>
          <PageToolbar>
            <BackButton>К списку</BackButton>
          </PageToolbar>
          <PageHeader
            title="Общее оформление страницы"
            titleAs="h2"
            description="Описание отчёта занимает общую ширину с таблицами и поясняет, какие данные сравниваются на этой странице."
            descriptionWidth="full"
            actions={<Button>Основное действие</Button>}
          />
          <Section reading title="Раздел для чтения">
            <p>
              Справка, методика, отчёт и фрагмент источника используют одну ширину и типографику.
              Заголовок и действия берутся из общих компонентов.
            </p>
          </Section>
        </ReadingContent>
        <SampleGroup label="Приветствие на главной · PageTitle welcome">
          <PageTitle as="h2" variant="welcome" className="text-center">
            Что будем исследовать?
          </PageTitle>
        </SampleGroup>
        <div className="grid gap-6 sm:grid-cols-2">
          <SampleGroup label="Стандартные отступы">
            <Card asChild className="w-full">
              <article aria-label="Источники гипотезы">
                <CardHeader>
                  <CardTitle>Источники гипотезы</CardTitle>
                  <CardDescription>Документы, на которых основана оценка.</CardDescription>
                </CardHeader>
                <CardContent>
                  <EvidenceTally supports={8} contradicts={2} mentions={4} verified />
                </CardContent>
              </article>
            </Card>
          </SampleGroup>
          <SampleGroup label="Компактные отступы · sm">
            <Card size="sm" className="w-full">
              <CardHeader>
                <CardTitle>Компактная карточка</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">
                  Та же структура с меньшими отступами.
                </p>
              </CardContent>
            </Card>
          </SampleGroup>
        </div>
        <CardButton
          aria-pressed={selected}
          onClick={() => setSelected((value) => !value)}
          className="p-5"
        >
          <span className="font-medium">{selected ? "Гипотеза выбрана" : "Выбрать гипотезу"}</span>
          <span className="mt-1 block text-sm text-muted-foreground">
            Вся поверхность — доступная кнопка.
          </span>
        </CardButton>
        <SettingsCard
          title="Настройки поиска"
          icon={SlidersHorizontal}
          description="Параметры текущей сессии."
          footer={<Badge variant="outline">Демонстрация</Badge>}
        >
          <StatStrip
            items={[
              { label: "Фрагменты", value: "24" },
              { label: "Порог", value: "0.87" },
              { label: "Режим", value: "Гибридный" },
            ]}
          />
        </SettingsCard>
        <Section title="Доказательства" description="Оценка опирается на найденные фрагменты.">
          <SegmentBar value={6} total={9} label="Уровень готовности 6 из 9" />
        </Section>
        <SampleGroup label="Даты как отсчёт прошедшего времени · демонстрационные значения">
          <DateStrip
            items={[
              { label: "Первое появление в открытых источниках", date: "2025-08-01" },
              { label: "Раннее внешнее подтверждение", empty: "не выявлено" },
              { label: "Начало текущего роста", date: "2026-08" },
              { label: "Обнаружено системой", date: "2026-09-21T03:34:08Z" },
            ]}
          />
        </SampleGroup>
        <SampleGroup label="Вкладки-счётчики · число и переход к списку одним жестом">
          <div className="w-full space-y-3">
            <MetricTabs
              id="kit-metric-tabs"
              active={sourceTab}
              onChange={setSourceTab}
              items={SOURCE_TABS}
            />
            <TabPanel tabsId="kit-metric-tabs" active={sourceTab} className="text-sm">
              {SOURCE_TABS.find((item) => item.id === sourceTab)?.label} · демонстрация.
            </TabPanel>
          </div>
        </SampleGroup>
        <SampleGroup label="Выдача технологий · счётчики отбора и карточки одной формы">
          <div className="w-full space-y-4">
            <CounterStrip
              items={[
                { label: "Просмотрено в окне", value: "380 042" },
                { label: "Источников обработано", value: "549" },
                { label: "Кандидатов", value: "64" },
                { label: "Отобрано", value: "12", hint: "Прошли все проверки отбора." },
                { label: "Исключено", value: "52" },
              ]}
            />
            <ResultGrid ordered tiles>
              <ResultItem>
                <CardButton className="flex flex-1 flex-col">
                  <ResultTile
                    media={
                      <ResultTileMedia
                        topLeft={<Badge variant="complement-solid">Слабый сигнал</Badge>}
                        topRight={<Badge variant="media">57&nbsp;%</Badge>}
                        bottomLeft={
                          <>
                            <Badge variant="brand-solid">×4 за полгода</Badge>
                            <Badge variant="ok-solid">Пилот</Badge>
                          </>
                        }
                      />
                    }
                    note={<span className="text-ok">Прошла все проверки радара</span>}
                    title="Иерархическая память агентов"
                    meta="6 источников · впервые в 2025 г."
                    action="Открыть разбор"
                  />
                </CardButton>
              </ResultItem>
              <ResultItem>
                <Card className="flex flex-1 flex-col">
                  <ResultCard
                    chips={<Badge variant="outline">Зарождение</Badge>}
                    title="Защита памяти от отравления"
                    footer={<ResultMeta icon={FileText} label="Работ" value="3" />}
                  >
                    <p className="line-clamp-4 text-sm text-muted-foreground">
                      <span className="font-medium">Мало свидетельств. </span>
                      Три работы за полгода, ни одной записи рынка.
                    </p>
                  </ResultCard>
                </Card>
              </ResultItem>
            </ResultGrid>
          </div>
        </SampleGroup>
        <Panel
          title="План эксперимента"
          collapsible
          defaultOpen={false}
          count={2}
          titleHint="Проверка выполняется на сопоставимых образцах."
        >
          <Field label="Критерий успеха">Рост прочности при сохранении массы.</Field>
          <FieldLabel>Следующие шаги</FieldLabel>
          <BulletList items={["Подготовить контрольную группу.", "Повторить измерения."]} />
        </Panel>
        <PageLayout className="px-0 py-0 md:px-0 md:py-0">
          <div className="grid grid-cols-2 gap-6 border-y">
            <MetricTile
              variant="plain"
              label="Документы"
              value="1 280"
              sub="Общая ширина страницы"
            />
            <MetricTile
              variant="plain"
              label="Готовность"
              value="98%"
              sub="Сводка без вложенных карточек"
            />
          </div>
        </PageLayout>
        <Disclosure title="Дополнительные параметры">
          <p className="text-sm text-muted-foreground">
            Второстепенные настройки открываются по запросу.
          </p>
        </Disclosure>
        <div className="grid gap-6 sm:grid-cols-2">
          <MetricTile
            label="Документы"
            value="2 438"
            sub="Демонстрационная выборка"
            icon={Database}
          />
          <MetricTile label="Проверено" value="38" sub="За выбранный период" />
        </div>
      </Specimen>
      <Specimen
        id="data"
        title="Таблицы и измерения"
        description="Тип файла определяется подписью и иконкой. Значение оценки показывается числом вместе со шкалой. Пагинация ограничивает количество видимых строк."
        code={
          "<Table><TableHeader><TableRow><TableHead>Источник</TableHead></TableRow></TableHeader>\n  <TableBody>{rows}</TableBody>\n</Table>\n<Pagination page={page} pageCount={12} onPage={setPage} />"
        }
      >
        <DonutChart
          label="Тематики документов · демонстрация"
          valueLabel="документов"
          data={[
            { key: "medicine", label: "Медицина", value: 420, color: "var(--chart-1)" },
            { key: "energy", label: "Энергетика", value: 280, color: "var(--chart-2)" },
            {
              key: "agriculture",
              label: "Сельское хозяйство",
              value: 180,
              color: "var(--chart-3)",
            },
            {
              key: "unknown",
              label: "Тематика не определена",
              value: 120,
              color: "var(--category-unassigned)",
            },
          ]}
        />
        <BarChart
          label="Запросы по дням · демонстрация"
          data={[
            { label: "1 сентября", value: 12, displayValue: "12 запросов" },
            { label: "2 сентября", value: 0, displayValue: "0 запросов" },
            { label: "3 сентября", value: 28, displayValue: "28 запросов" },
            { label: "4 сентября", value: 8, displayValue: "8 запросов" },
          ]}
        />
        <SampleGroup label="Измерение на шкале 0–100 · отметка середины">
          <div className="w-full space-y-3">
            <p className="text-sm">Сравнение с базой · демонстрация: 72 из 100</p>
            <ValueScale value={72} label="Сравнение с базой: 72 из 100" />
          </div>
        </SampleGroup>
        <div className="flex flex-wrap gap-2">
          {(["pdf", "docx", "xlsx", "pptx", "txt", "eml", "image", "db", "other"] as const).map(
            (type) => (
              <FileTypeBadge key={type} type={type} />
            ),
          )}
        </div>
        <Table caption={`Демонстрационные источники, страница ${page}`}>
          <TableHeader>
            <TableRow>
              <TableHead>Источник</TableHead>
              <TableHead>Ответственный</TableHead>
              <TableHead>Состояние</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {[0, 1, 2].map((n) => (
              <TableRow key={n}>
                <TableCell>Исследование {(page - 1) * 3 + n + 1}</TableCell>
                <TableCell>
                  <span className="flex items-center gap-2">
                    <Avatar size="sm">
                      <AvatarFallback>АК</AvatarFallback>
                    </Avatar>
                    А. Ким
                  </span>
                </TableCell>
                <TableCell>
                  <Badge variant={n === 1 ? "warn" : "ok"}>
                    {n === 1 ? "Проверить" : "Готово"}
                  </Badge>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        <Pagination page={page} pageCount={12} onPage={setPage} />
        <SampleGroup label="Нативная прокрутка · клавиатура и касание">
          <Card className="w-full">
            <ScrollArea aria-label="Пример прокрутки" className="h-32 space-y-2 p-4 text-sm">
              {Array.from({ length: 8 }, (_, index) => (
                <p key={index}>Измерение {index + 1}: контрольный образец, температура 24 °C.</p>
              ))}
            </ScrollArea>
          </Card>
        </SampleGroup>
        <Separator />
        <div className="flex flex-wrap gap-5">
          {(["brand", "ok", "warn", "risk", "ink"] as const).map((tone) => (
            <div key={tone}>
              <span className="mb-2 block text-xs">{tone} · 6 / 9</span>
              <SegmentBar tone={tone} value={6} />
            </div>
          ))}
        </div>
        <EvidenceTally supports={0} contradicts={0} mentions={0} verified={false} />
        <EvidenceTally supports={8} contradicts={2} mentions={4} verified />
        <SampleGroup label="Легенда · маркер и подпись">
          <LegendItem marker="bg-chart-1">Рост 0–50 %</LegendItem>
          <LegendItem marker="bg-chart-3" size="md">
            Более 300 %
          </LegendItem>
          <LegendItem outline="dashed" size="md" className="text-muted-foreground">
            Найдено автоматически
          </LegendItem>
          <LegendItem outline="solid" size="md" className="text-muted-foreground">
            Запрос оператора
          </LegendItem>
          <LegendItem marker="bg-foreground" size="xs" shape="square">
            Прочая работа
          </LegendItem>
          <LegendItem color="var(--warn)">Вычисленный цвет</LegendItem>
        </SampleGroup>
      </Specimen>
      <Specimen
        id="content"
        title="Научный текст"
        description="RichText — единый вход для Markdown, таблиц, формул и химии. Парсер загружается по требованию. Исходный HTML не исполняется."
        code={"<RichText>{markdownFromApi}</RichText>\n<AiNote />"}
      >
        <RichText>
          {
            "**План проверки.** Сравнить две группы при одинаковой температуре.\n\n- Зафиксировать исходное состояние.\n- Измерить изменение прочности.\n\nНормированная оценка: $s = \\frac{x - x_{min}}{x_{max} - x_{min}}$.\n\nХимическая запись: $\\ce{H2O}$.\n\n| Измерение | Значение |\n| --- | --- |\n| Температура | 24 °C |\n| Повторения | 6 |"
          }
        </RichText>
        <AiNote />
        <Button
          variant="outline"
          className="justify-self-start"
          aria-expanded={showPdf}
          aria-controls="kit-pdf"
          onClick={() => setShowPdf((value) => !value)}
        >
          {showPdf ? "Закрыть пример PDF" : "Открыть пример PDF"}
        </Button>
        <div id="kit-pdf" hidden={!showPdf}>
          {showPdf && (
            <div className="h-[28rem]">
              <PdfViewer url={demoPdf} />
            </div>
          )}
        </div>
        <Button asChild variant="link" size="inline" className="justify-self-start">
          <a href="#feedback">Состояния загрузки и ошибок</a>
        </Button>
      </Specimen>
      <Specimen
        id="export"
        title="Выгрузка документа"
        description="ReportExport — одна кнопка «Экспорт» с выбором формата на всех экранах продукта. Документ передаётся функцией и собирается только по выбору: из него получаются Word и печать в PDF. Таблицы и свои форматы идут списком items над ними; serverFiles даёт CSV и JSON, которые собирает сервер."
        code={
          '<ReportExport items={[{ format: "csv", run: saveTable }]} doc={() => reportDoc} onPrint={openPrintPage} />'
        }
      >
        <div className="justify-self-start">
          <ReportExport
            items={[
              {
                format: "csv",
                run: () => saveFile("demo.csv", "Направление;Рост\nКомпозиты;18 %\n", "text/csv"),
              },
            ]}
            doc={() => DEMO_REPORT}
          />
        </div>
        <div className="max-w-4xl">
          <ReportBlocks blocks={DEMO_REPORT.blocks} />
        </div>
      </Specimen>
    </>
  );
}
