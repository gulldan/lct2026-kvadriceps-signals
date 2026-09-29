import { useState } from "react";

import { Disclosure } from "@/shared/ui/Disclosure";
import { Label } from "@/shared/ui/Label";
import { Notice } from "@/shared/ui/Notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import corpus from "../../tests/fixtures/signalgraph-corpus-v1.json";
import { Specimen } from "./Specimen";

const kindLabels: Record<string, string> = {
  paper: "Научная работа",
  company_material: "Материал компании",
  software_repository: "Программная реализация",
  patent: "Патентная публикация",
  hypothesis: "Сгенерированная гипотеза",
};
const scopeLabels: Record<string, string> = {
  public: "Открытый источник",
  shared: "Общий корпус",
  owner: "Только владелец",
};

export function TrendRadarFixtures() {
  const [selected, setSelected] = useState("early-1");
  const record = corpus.records.find(({ id }) => id === selected)!;
  const source = record.source;
  const fields = [
    ["Тип", kindLabels[source.source_kind]],
    ["Провайдер", source.provider],
    ["Дата публикации", source.publication?.value ?? "Неизвестна"],
    ["Точность даты", source.publication?.precision ?? "Не определена"],
    ["Доступ", scopeLabels[source.access_scope]],
    ["Канонический ID", source.canonical_id],
    ["Наблюдение", source.observed_at],
    ["Ревизия", source.revision],
  ];

  return (
    <Specimen
      id="trend-fixtures"
      title="Источники трендов"
      description="Контрольный корпус v1 для TR-004/005: происхождение, точность дат, версии и независимые единицы учета."
      code="backend/platform/pkg/sourcedoc/testdata/v1/corpus.json — исходный corpus и oracle. Копия в frontend/tests/fixtures проверяется на точное совпадение."
    >
      <Notice tone="brand">
        Синтетические данные · {corpus.records.length} записей. Тестовые идентификаторы и ожидаемые
        исходы для проверки контракта.
      </Notice>
      <div className="grid gap-2">
        <Label htmlFor="trend-fixture-select">Контрольная запись</Label>
        <Select value={selected} onValueChange={setSelected}>
          <SelectTrigger id="trend-fixture-select">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {corpus.records.map(({ id }) => (
              <SelectItem key={id} value={id}>
                {id}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div aria-live="polite" aria-atomic="true">
        <p className="font-medium">{record.id}</p>
        {corpus.oracle.excluded_future_revisions.includes(record.id) && (
          <p className="mt-2 text-sm text-risk">
            Исключена из среза: эта ревизия появилась после 31 августа 2026 года.
          </p>
        )}
      </div>
      <dl className="grid gap-x-8 gap-y-5 sm:grid-cols-2">
        {fields.map(([label, value]) => (
          <div key={label} className="min-w-0">
            <dt className="text-sm text-muted-foreground">{label}</dt>
            <dd className="mt-1 wrap-anywhere">{value}</dd>
          </div>
        ))}
      </dl>
      {source.github && (
        <p className="text-sm text-muted-foreground">
          Stars: {source.github.stars ?? "Неизвестно"} · наблюдение {source.observed_at}. История до
          наблюдения неизвестна.{" "}
          {source.github.fork
            ? `Fork репозитория ${source.github.root_id}.`
            : "Исходный репозиторий."}
        </p>
      )}
      <div>
        <h3 className="mb-2 font-semibold">Контрольный фрагмент</h3>
        <blockquote className="max-w-prose text-base leading-relaxed wrap-anywhere">
          {record.text}
        </blockquote>
        <p className="mt-2 text-sm text-muted-foreground">
          {record.anchors[0]?.locator} · символы {record.anchors[0]?.start}–{record.anchors[0]?.end}
        </p>
      </div>
      <Disclosure title="Ожидаемые числа ранней темы">
        <Table aria-label="Контрольные квартальные значения">
          <TableHeader>
            <TableRow>
              <TableHead>Квартал</TableHead>
              <TableHead>Научные работы</TableHead>
              <TableHead>Покрытие</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {Object.entries(corpus.oracle.early_quarter_counts).map(([period, count]) => (
              <TableRow key={period}>
                <TableCell>{period}</TableCell>
                <TableCell>{count ?? "Неизвестно"}</TableCell>
                <TableCell>{count === null ? "Нет наблюдения" : "Полный период"}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Disclosure>
      <Disclosure title="Контракт источника и oracle">
        <pre className="max-h-96 overflow-auto rounded-md bg-secondary p-4 font-mono text-xs leading-relaxed">
          {JSON.stringify({ source, oracle: corpus.oracle }, null, 2)}
        </pre>
      </Disclosure>
    </Specimen>
  );
}
