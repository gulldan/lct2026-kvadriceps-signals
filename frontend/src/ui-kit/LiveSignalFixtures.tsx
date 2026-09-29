import { useState } from "react";
import { MemoryRouter } from "react-router-dom";

import { Button } from "@/shared/ui/Button";
import { ExecutiveDialog } from "@/features/trend/ui/ExecutiveDialog";
import { LiveCandidates, type CandidatesView } from "@/features/trend/ui/LiveCandidates";
import { LiveLimits } from "@/features/trend/ui/LiveLimits";
import { LiveSignalReport } from "@/features/trend/ui/LiveSignalReport";
import type { LiveDetail, LiveExecutive, LiveLimit } from "@/features/trend/liveApi";
import detail from "../../tests/fixtures/live-search-v1.json";
import { Specimen } from "./Specimen";

const LIMITS: LiveLimit[] = [
  { code: "arxiv_unavailable", text: "arxiv: request failed" },
  { code: "scholar_capped", values: { taken: 999, found: 10029 }, text: "scholar: capped" },
  { code: "scholar_journals", values: { added: 1161 }, text: "scholar: journals added" },
  { code: "classifier_not_connected", text: "classifier: not connected" },
  { code: "qualification_unavailable", values: { candidates: 4 }, text: "qualification: budget" },
  { code: "affiliations_budget", values: { checked: 64, pending: 14153 }, text: "affiliations" },
];

function executiveFixture(answer: LiveDetail): LiveExecutive {
  const rows = [
    {
      step: "pilot",
      horizon: "0-1",
      benefit: "ИТ-инфраструктура: предиктивное обслуживание серверов и банкоматов по телеметрии.",
      reason: "В карточке есть промышленный кейс и зрелый рынок.",
    },
    {
      step: "pilot",
      horizon: "1-3",
      benefit: "Клиентский сервис: дообучаемые модели для разбора обращений.",
      reason: "Стадия внедрения — пилоты у нескольких компаний.",
    },
    {
      step: "watch",
      horizon: "1-3",
      benefit: "Антифрод: автоматический контроль качества решений скоринга.",
      reason: "Работ мало, рынок только формируется.",
    },
    {
      step: "watch",
      benefit: "",
      reason: "Связь с банком по карточке не прослеживается, но тема растёт.",
    },
    {
      step: "reject",
      horizon: "3+",
      benefit: "",
      reason: "Промышленная тема без применения в банке.",
    },
  ] as const;
  const candidates = answer.candidates ?? [];
  return {
    run_id: "demo",
    query: "искусственный интеллект в промышленности",
    observed_at: answer.observed_at,
    sources_processed: answer.counters?.sources_processed ?? 0,
    candidates: answer.counters?.candidates ?? 0,
    selected: candidates.length,
    items: rows.map((row, index) =>
      Object.assign(
        {
          id: candidates[index]?.id ?? String(index),
          name: candidates[index]?.label ?? "",
          summary: candidates[index]?.card?.summary,
        },
        row,
      ),
    ),
    model: "demo-model",
    written_at: answer.observed_at,
  };
}

export function LiveSignalFixtures() {
  const answer = detail as unknown as LiveDetail;
  const candidates = answer.candidates ?? [];
  const [open, setOpen] = useState<number | null>(null);
  const [view, setView] = useState<CandidatesView>("cards");
  const candidate = open === null ? undefined : candidates[open];
  const [executive, setExecutive] = useState(false);
  return (
    <div className="space-y-8">
      <Specimen
        id="live-executive"
        title="Справка для руководителя"
        description="Ответ на вопрос «что внести в план»: сверху расклад решений, ниже технологии по шагу — пилот, наблюдение, отказ. У каждой польза для банка и основание шага рядом, горизонт плашкой, пропуск модели назван словами."
        code="<ExecutiveDialog open={open} onOpenChange={setOpen} detail={detail} query={query} />"
      >
        <MemoryRouter>
          <Button variant="outline" onClick={() => setExecutive(true)}>
            Открыть справку
          </Button>
          <ExecutiveDialog
            open={executive}
            onOpenChange={setExecutive}
            detail={answer}
            query="искусственный интеллект в промышленности"
            write={async () => executiveFixture(answer)}
          />
        </MemoryRouter>
      </Specimen>
      <Specimen
        id="live-limits"
        title="Границы сбора и отказы источников"
        description="Оговорки ответа разделены по виду: канал, который не собрался, тревожит, а свойства метода — окно выборки, неподключённый классификатор, добор журналов — стоят приглушённым блоком, и лишние строки уходят под «Показать ещё»."
        code="<LiveLimits limits={limits} />"
      >
        <LiveLimits limits={LIMITS} />
      </Specimen>
      <Specimen
        id="live-signals"
        title="Выдача по открытому запросу"
        description="Содержимое вкладки «Найденные технологии» в карточке направления: доля выполненных правил, непроверенные условия и кандидаты. Снимок намеренно сохраняет старые неполные счётчики для проверки предупреждения."
        code="<LiveCandidates detail={detail} view={view} onView={setView} onOpen={open} covers={false} />"
      >
        <LiveCandidates
          detail={answer}
          view={view}
          onView={setView}
          onOpen={(row) => setOpen(candidates.findIndex(({ id }) => id === row.id))}
          covers={false}
        />
      </Specimen>
      <Specimen
        id="live-signal-report"
        title="Инсайт как документ-отчёт"
        description="Просмотр технологии: описание, преимущество, кейс-пример, объяснение статуса и источники с паспортом. Приборная панель вынесена в отдельную ссылку."
        code="<LiveSignalReport detail={detail} candidate={candidate} rank={1} total={15} />"
      >
        {candidate ? (
          // Отчёт ссылается на страницу проверки модели, а своего маршрутизатора у витрины нет.
          <MemoryRouter>
            <LiveSignalReport
              detail={answer}
              candidate={candidate}
              query={answer.topic.name}
              rank={(open ?? 0) + 1}
              total={candidates.length}
              onBack={() => setOpen(null)}
              onMeasurements={() => setOpen(null)}
            />
          </MemoryRouter>
        ) : (
          <Button variant="outline" onClick={() => setOpen(0)}>
            Открыть первую технологию выдачи
          </Button>
        )}
      </Specimen>
    </div>
  );
}
