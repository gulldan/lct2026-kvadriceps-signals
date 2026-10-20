import { useRequest } from "@/shared/lib/useRequest";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { toast } from "@/shared/ui/Sonner";

import {
  hypothesisJobErrorMessage,
  listHypothesisJobs,
  type ApiHypothesisJob,
} from "@/features/hypothesis";
import { getSystemActivity, workerState, type SystemActivity } from "@/features/task/api";
import { i18n } from "@/shared/i18n";

type BackgroundTaskStatus = "running" | "success" | "error";

export interface BackgroundTask {
  id: string;
  jobID?: string;
  key?: string;
  title: string;
  description?: string;
  status: BackgroundTaskStatus;
  startedAt: string;
  finishedAt?: string;
  error?: string;
}

interface RunTaskOptions<T> {
  key?: string;
  title: string;
  description?: string;
  successMessage?: string | ((result: T) => string);
  errorMessage?: string | ((error: unknown) => string);
  run: (onJobCreated: (jobID: string) => void) => Promise<T>;
  onSuccess?: (result: T) => void;
  onError?: (error: unknown) => void;
}

interface TaskContextValue {
  tasks: BackgroundTask[];
  activeCount: number;
  activity: SystemActivity | null;
  started: boolean;
  runTask: <T>(options: RunTaskOptions<T>) => Promise<T | undefined>;
  isTaskRunning: (key: string) => boolean;
  clearFinished: () => void;
  refreshActivity: () => void;
}

const TaskContext = createContext<TaskContextValue | null>(null);

function taskID(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : i18n.t("task:toasts.failed");
}

export const JOB_TITLES: Record<string, string> = {
  get generate() {
    return i18n.t("task:jobs.generate");
  },
  get verify() {
    return i18n.t("task:jobs.verify");
  },
  get enrich() {
    return i18n.t("task:jobs.enrich");
  },
  get assess_trl() {
    return i18n.t("task:jobs.assess_trl");
  },
  get competitors() {
    return i18n.t("task:jobs.competitors");
  },
  get refine() {
    return i18n.t("task:jobs.refine");
  },
  get tag() {
    return i18n.t("task:jobs.tag");
  },
};

const AUTO_JOB_KINDS = new Set(["verify", "assess_trl"]);
const RUNNING_JOB_FRESH_MS = 120_000;

function shouldShowBackendJob(job: ApiHypothesisJob): boolean {
  if (!AUTO_JOB_KINDS.has(job.kind)) return true;
  if (job.status !== "running") return false;
  const heartbeat = Date.parse(job.heartbeat_at || job.started_at || job.created_at);
  return Number.isFinite(heartbeat) && Date.now() - heartbeat <= RUNNING_JOB_FRESH_MS;
}

export function taskFromJob(job: ApiHypothesisJob): BackgroundTask {
  return {
    id: `job:${job.id}`,
    jobID: job.id,
    key: `job:${job.id}`,
    title: JOB_TITLES[job.kind] ?? i18n.t("task:jobs.fallback"),
    description: job.input.kpi_title || job.input.hypothesis_id || job.input.kpi_id,
    status: job.status === "failed" ? "error" : job.status === "succeeded" ? "success" : "running",
    startedAt: job.started_at || job.created_at,
    finishedAt: job.finished_at,
    error: job.status === "failed" ? hypothesisJobErrorMessage(job.error) : undefined,
  };
}

/** Опрос того же серверного задания не должен создавать второе уведомление. */
export function mergeTasks(tasks: BackgroundTask[]): BackgroundTask[] {
  const localJobs = new Set(
    tasks
      .filter((task) => !task.id.startsWith("job:"))
      .flatMap((task) => (task.jobID ? [task.jobID] : [])),
  );
  return tasks.filter((task) => !task.id.startsWith("job:") || !localJobs.has(task.jobID ?? ""));
}

function message<T>(
  value: string | ((result: T) => string) | undefined,
  result: T,
  fallback: string,
): string {
  return typeof value === "function" ? value(result) : (value ?? fallback);
}

function errorMessage(
  value: string | ((error: unknown) => string) | undefined,
  error: unknown,
): string {
  return typeof value === "function" ? value(error) : (value ?? errorText(error));
}

export function TaskProvider({ children }: { children: ReactNode }) {
  const [tasks, setTasks] = useState<BackgroundTask[]>([]);
  // Оба запроса отнимают у первого экрана соединения к хосту, а колокольчик подождёт простоя.
  const [started, setStarted] = useState(false);
  useEffect(() => {
    const idle = window.requestIdleCallback ?? ((fn: () => void) => window.setTimeout(fn, 1500));
    const cancel = window.cancelIdleCallback ?? window.clearTimeout;
    const handle = idle(() => setStarted(true), { timeout: 4000 });
    return () => cancel(handle);
  }, []);
  // Отказ эндпоинта на старом бэкенде или до входа оставляет прошлое значение.
  const activityRequest = useRequest<SystemActivity>(
    started ? "system-activity" : null,
    getSystemActivity,
    { pollMs: 30_000, keepPrevious: true },
  );
  const activity = activityRequest.data ?? null;
  const jobsRequest = useRequest(started ? "hypothesis-jobs" : null, () => listHypothesisJobs(20), {
    pollMs: (jobs) => (jobs.some((job) => job.status === "running") ? 5000 : 20_000),
    keepPrevious: true,
  });
  const jobs = jobsRequest.data;
  useEffect(() => {
    if (!jobs) return;
    const backendTasks = jobs.filter(shouldShowBackendJob).map(taskFromJob);
    setTasks((prev) => {
      const localTasks = prev.filter((task) => !task.id.startsWith("job:"));
      return mergeTasks([...localTasks, ...backendTasks]).slice(0, 30);
    });
  }, [jobs]);

  const runTask = useCallback(async <T,>(options: RunTaskOptions<T>): Promise<T | undefined> => {
    const id = taskID();
    const startedAt = new Date().toISOString();
    setTasks((prev) =>
      [
        {
          id,
          key: options.key,
          title: options.title,
          description: options.description,
          status: "running" as const,
          startedAt,
        },
        ...prev,
      ].slice(0, 30),
    );

    try {
      const result = await options.run((jobID) => {
        setTasks((prev) =>
          mergeTasks(prev.map((task) => (task.id === id ? { ...task, jobID } : task))),
        );
      });
      setTasks((prev) =>
        prev.map((task) =>
          task.id === id
            ? { ...task, status: "success", finishedAt: new Date().toISOString() }
            : task,
        ),
      );
      options.onSuccess?.(result);
      toast.success(message(options.successMessage, result, i18n.t("task:toasts.done")));
      return result;
    } catch (error) {
      const text = errorMessage(options.errorMessage, error);
      setTasks((prev) =>
        prev.map((task) =>
          task.id === id
            ? { ...task, status: "error", finishedAt: new Date().toISOString(), error: text }
            : task,
        ),
      );
      options.onError?.(error);
      toast.error(text);
      return undefined;
    }
  }, []);

  const isTaskRunning = useCallback(
    (key: string) => tasks.some((task) => task.key === key && task.status === "running"),
    [tasks],
  );

  const clearFinished = useCallback(() => {
    setTasks((prev) => prev.filter((task) => task.status === "running"));
  }, []);

  const activeCount =
    tasks.filter((task) => task.status === "running").length +
    (activity?.workers.filter((w) => workerState(w) === "running").length ?? 0);
  const value = useMemo(
    () => ({
      tasks,
      activeCount,
      activity,
      started,
      runTask,
      isTaskRunning,
      clearFinished,
      refreshActivity: activityRequest.retry,
    }),
    [
      tasks,
      activeCount,
      activity,
      started,
      runTask,
      isTaskRunning,
      clearFinished,
      activityRequest.retry,
    ],
  );

  return <TaskContext.Provider value={value}>{children}</TaskContext.Provider>;
}

export function useBackgroundTasks(): TaskContextValue {
  const ctx = useContext(TaskContext);
  if (!ctx) throw new Error("useBackgroundTasks must be used within TaskProvider");
  return ctx;
}
