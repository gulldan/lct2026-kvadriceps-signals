import { Bell, CheckCircle2, CircleAlert, Loader2, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { useBackgroundTasks, type BackgroundTask } from "@/features/task/TaskContext";
import {
  WORKER_LABELS,
  workerState,
  type SystemActivity,
  type WorkerStatus,
} from "@/features/task/api";
import { ackWatch, useWatches, WatchDigests, type Watch } from "@/features/watch";
import { statusTone, toneClasses } from "@/shared/design-system/tones";
import { cn } from "@/shared/lib/cn";
import { formatTime } from "@/shared/lib/format";
import { Button } from "@/shared/ui/Button";
import { Kicker } from "@/shared/ui/Kicker";
import { ScrollArea } from "@/shared/ui/ScrollArea";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/shared/ui/DropdownMenu";

function StatusIcon({ state, className }: { state: string; className?: string }) {
  const tone = statusTone(state);
  const Icon = tone === "brand" ? Loader2 : tone === "ok" ? CheckCircle2 : CircleAlert;
  return (
    <Icon
      className={cn(
        "size-4",
        tone === "brand" && "animate-spin",
        toneClasses[tone].text,
        className,
      )}
      aria-hidden
    />
  );
}

function taskTime(task: BackgroundTask): string {
  return formatTime(task.finishedAt ?? task.startedAt);
}

function WorkerRow({ worker }: { worker: WorkerStatus }) {
  const { t } = useTranslation("task");
  const state = workerState(worker);
  const label = WORKER_LABELS[worker.name] ?? worker.name;
  const progress =
    worker.progressTotal && worker.progressTotal > 0
      ? ` ${worker.progressDone ?? 0}/${worker.progressTotal}`
      : "";
  return (
    <div className="flex items-center gap-2 rounded-md px-2 py-1.5">
      <StatusIcon state={state} className="shrink-0" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm">
          {label}
          {state === "running" && (
            <span className="font-mono text-xs text-muted-foreground">{progress}</span>
          )}
        </p>
        {(state === "stale" || state === "unknown") && (
          <p className="mt-0.5 text-xs text-muted-foreground">{t(`workerState.${state}`)}</p>
        )}
        {worker.state === "error" && worker.lastError && (
          <p className="mt-0.5 line-clamp-2 text-xs text-risk">{worker.lastError}</p>
        )}
      </div>
    </div>
  );
}

function SystemPulse({ activity }: { activity: SystemActivity }) {
  const { t } = useTranslation("task");
  const busy = activity.workers.filter((w) => workerState(w) !== "idle");
  if (activity.workers.length === 0) return null;
  return (
    <>
      <Kicker className="px-2 pt-2 pb-1">{t("notifications.system")}</Kicker>
      {busy.map((w) => (
        <WorkerRow key={w.name} worker={w} />
      ))}
      {busy.length === 0 && (
        <p className="px-2 pb-1.5 text-xs text-muted-foreground">{t("notifications.allDone")}</p>
      )}
      <DropdownMenuSeparator />
    </>
  );
}

function TaskRow({ task }: { task: BackgroundTask }) {
  return (
    <div className="flex gap-2 rounded-md px-2 py-2">
      <div className="mt-0.5 shrink-0">
        <StatusIcon state={task.status} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-start justify-between gap-2">
          <p className="truncate text-sm font-medium">{task.title}</p>
          <span className="shrink-0 text-caption text-muted-foreground">{taskTime(task)}</span>
        </div>
        {task.description && (
          <p className="mt-0.5 line-clamp-2 text-xs text-muted-foreground">{task.description}</p>
        )}
        {task.error && <p className="mt-0.5 line-clamp-2 text-xs text-destructive">{task.error}</p>}
      </div>
    </div>
  );
}

export function TaskNotifications() {
  const { t } = useTranslation("task");
  const { tasks, activeCount, activity, started, clearFinished } = useBackgroundTasks();
  const finishedCount = tasks.filter((task) => task.status !== "running").length;
  const [open, setOpen] = useState(false);
  // При открытии подписки обновляются: сводка могла прийти между опросами.
  const { watches, refresh } = useWatches(started);
  const unread = watches.filter((watch) => watch.digest).length;
  const badge = activeCount + unread;
  const bellLabel =
    [
      activeCount > 0 ? t("notifications.bellAria", { count: activeCount }) : "",
      unread > 0 ? t("notifications.bellAriaWatch", { count: unread }) : "",
    ]
      .filter(Boolean)
      .join(", ") || t("notifications.bellAriaIdle");
  const openWatch = (watch: Watch) => {
    setOpen(false);
    if (watch.digest) void ackWatch(watch).then(refresh, refresh);
  };

  return (
    <DropdownMenu
      modal={false}
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (next) refresh();
      }}
    >
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          className="relative"
          aria-haspopup="dialog"
          aria-label={bellLabel}
        >
          <Bell className="size-4" aria-hidden />
          {badge > 0 && (
            <span
              className={cn(
                "absolute -top-0.5 -right-0.5 flex min-w-4 items-center justify-center rounded-full",
                "bg-primary px-1 text-micro leading-4 font-semibold text-primary-foreground",
              )}
            >
              {badge}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        semantics="dialog"
        aria-label={t("notifications.title")}
        className="w-80"
      >
        <DropdownMenuLabel className="flex items-center justify-between gap-2">
          <span>{t("notifications.title")}</span>
          {activeCount > 0 && (
            <span className="rounded-full bg-muted px-1.5 py-0.5 text-caption font-medium text-muted-foreground">
              {t("notifications.inProgress", { count: activeCount })}
            </span>
          )}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {watches.length > 0 && <WatchDigests watches={watches} onOpen={openWatch} />}
        {activity && <SystemPulse activity={activity} />}
        {tasks.length === 0 && !activity?.workers.length ? (
          // До первого опроса пустота ничего не утверждает: «нет задач» сменилось бы списком.
          started && (
            <p className="px-2 py-5 text-center text-sm text-muted-foreground">
              {t("notifications.empty")}
            </p>
          )
        ) : (
          <ScrollArea aria-label={t("notifications.title")} className="max-h-80">
            {tasks.map((task) => (
              <TaskRow key={task.id} task={task} />
            ))}
          </ScrollArea>
        )}
        {finishedCount > 0 && (
          <>
            <DropdownMenuSeparator />
            <Button
              variant="ghost"
              size="sm"
              className="w-full justify-start"
              onClick={clearFinished}
            >
              <Trash2 className="size-4" aria-hidden />
              {t("notifications.clearFinished")}
            </Button>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
