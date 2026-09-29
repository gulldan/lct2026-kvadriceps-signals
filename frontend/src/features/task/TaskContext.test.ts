import { beforeAll, expect, spyOn, test } from "bun:test";
import { generateHypotheses, type ApiHypothesisJob } from "@/features/hypothesis";
import { initI18n } from "@/shared/i18n";
import { mergeTasks, taskFromJob, type BackgroundTask } from "./TaskContext";

beforeAll(async () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "document");
  Object.defineProperty(globalThis, "document", {
    configurable: true,
    value: { documentElement: { lang: "" } },
  });
  try {
    await initI18n();
  } finally {
    if (descriptor) Object.defineProperty(globalThis, "document", descriptor);
    else Reflect.deleteProperty(globalThis, "document");
  }
});

test("один серверный запуск объединяется с локальной задачей, разные запуски сохраняются", () => {
  const local: BackgroundTask = {
    id: "local-generation",
    key: "direction:solar-cells:generate",
    jobID: "server-job-1",
    title: "Генерация гипотез",
    description: "Направление: Solar Cells",
    status: "running",
    startedAt: "2026-09-18T13:45:00Z",
  };
  const remote: BackgroundTask = {
    ...local,
    id: "job:server-job-1",
    key: "job:server-job-1",
    description: "Solar Cells",
  };
  const another: BackgroundTask = {
    ...remote,
    id: "job:server-job-2",
    key: "job:server-job-2",
    jobID: "server-job-2",
  };

  const tasks = mergeTasks([remote, another, local]);
  expect(tasks).toEqual([another, local]);
  expect(tasks.find((task) => task.key === local.key)).toBe(local);
  expect(tasks.filter((task) => task.status === "running")).toHaveLength(2);
  expect(mergeTasks([remote, another])).toEqual([remote, another]);

  const finished: BackgroundTask = { ...local, status: "success" };
  const afterCompletion = mergeTasks([remote, another, finished]);
  expect(afterCompletion).toEqual([another, finished]);
  expect(afterCompletion.filter((task) => task.status === "running")).toHaveLength(1);
});

test("отказ из-за источников понятен в уведомлении и ожидании, неизвестная ошибка скрывает код", async () => {
  const fetch = spyOn(globalThis, "fetch");
  try {
    for (const error of ["insufficient_context", "operation_failed", "future_error", undefined]) {
      const job: ApiHypothesisJob = {
        id: "failed-hypothesis-job",
        owner_id: "user",
        kind: "generate",
        status: "failed",
        input: { kpi_id: "thesis" },
        result_ids: [],
        error,
        created_at: "2026-09-27T00:00:00Z",
      };
      const expected =
        error === "insufficient_context"
          ? "Недостаточно данных в источниках для дополнения гипотезы. Добавьте материалы или уточните формулировку."
          : "Не удалось выполнить задачу";
      expect(taskFromJob({ ...job, kind: "enrich" })).toMatchObject({
        status: "error",
        error: expected,
      });
      fetch.mockResolvedValue(Response.json(job));
      await expect(generateHypotheses({ kpi_id: "thesis" })).rejects.toMatchObject({
        status: error === "insufficient_context" ? 422 : 500,
        message: expected,
      });
      expect(job.status).toBe("failed");
    }
  } finally {
    fetch.mockRestore();
  }
});
