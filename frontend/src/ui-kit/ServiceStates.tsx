import { useState } from "react";

import { LiveSearchProgress } from "@/features/trend/ui/LiveSearchProgress";
import type { LiveSearchState } from "@/features/trend/liveApi";
import { Button } from "@/shared/ui/Button";
import { Segmented } from "@/shared/ui/Segmented";
import { StandaloneLayout } from "@/shared/ui/StandaloneLayout";
import { StatusPage, type PageStatus } from "@/shared/ui/StatusPage";
import { toast } from "@/shared/ui/Sonner";
import { Specimen } from "./Specimen";

const PROGRESS: Record<string, LiveSearchState> = {
  collecting: {
    query: "Новые технологии хранения энергии",
    status: "running",
    step: "patents",
    step_index: 5,
    elapsed_seconds: 95,
    wait_seconds: 20,
  },
  analysis: {
    query: "Новые технологии хранения энергии",
    status: "running",
    step: "analysis",
    step_index: 9,
    elapsed_seconds: 214,
    typical_seconds: 320,
    found: ["Твердотельные аккумуляторы", "Натрий-ионные батареи"],
  },
  overdue: {
    query: "Новые технологии хранения энергии",
    status: "running",
    step: "cards",
    step_index: 10,
    elapsed_seconds: 860,
    found: ["Твердотельные аккумуляторы"],
  },
  reopening: {
    query: "Новые технологии хранения энергии",
    status: "running",
    reopening: true,
    step: "rescore",
    elapsed_seconds: 2,
  },
};

export function ServiceStates() {
  const [status, setStatus] = useState("503");
  const [scenario, setScenario] = useState("analysis");
  return (
    <>
      <Specimen
        id="service-states"
        title="Страницы ошибок"
        description="Код ошибки становится знаком радара: закрытый сектор, потерянный сигнал, распавшийся ноль или пауза. На 404 стрелка следует за указателем. Отдельные страницы: /403, /404, /500 и /503."
        code="<StatusPage status={503} onRetry={retry} />"
      >
        <div className="flex flex-wrap items-center justify-between gap-3">
          <Segmented
            aria-label="Код ошибки"
            value={status}
            onChange={setStatus}
            options={["403", "404", "500", "503"].map((value) => ({ value, label: value }))}
          />
          <Button asChild variant="outline" size="sm">
            <a href={`/${status}`}>Открыть страницу</a>
          </Button>
        </div>
        <StandaloneLayout preview>
          <StatusPage
            status={Number(status) as PageStatus}
            titleAs="h2"
            onRetry={
              Number(status) >= 500 ? () => toast("Повторная попытка — пример действия") : undefined
            }
          />
        </StandaloneLayout>
      </Specimen>
      <Specimen
        id="search-progress"
        title="Длительный поиск"
        description="Демонстрационные состояния: этапы приходят с сервера, время не превращается в выдуманный процент. Открытие сохранённого ответа не запускает полный план заново."
        code="<LiveSearchProgress state={state} reconnecting={false} />"
      >
        <Segmented
          aria-label="Состояние поиска"
          value={scenario}
          onChange={setScenario}
          options={[
            { value: "collecting", label: "Сбор" },
            { value: "analysis", label: "Анализ" },
            { value: "overdue", label: "Дольше 14 минут" },
            { value: "reconnecting", label: "Обрыв связи" },
            { value: "reopening", label: "Сохранённый" },
          ]}
          className="flex-wrap"
        />
        <LiveSearchProgress
          key={scenario}
          state={PROGRESS[scenario] ?? PROGRESS.analysis!}
          reconnecting={scenario === "reconnecting"}
        />
      </Specimen>
    </>
  );
}
