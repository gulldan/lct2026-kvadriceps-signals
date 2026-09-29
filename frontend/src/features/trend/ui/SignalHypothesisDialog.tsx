import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useSearchParams } from "react-router-dom";
import { Plus, ArrowUpRight } from "lucide-react";
import { createHypothesis, type RadarBet } from "@/features/hypothesis";
import { Button } from "@/shared/ui/Button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { Input } from "@/shared/ui/Input";
import { Label } from "@/shared/ui/Label";
import { Notice } from "@/shared/ui/Notice";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/Sonner";
import {
  writeSignalBrief,
  type LiveDetail,
  type LiveSignalBrief,
  type LiveTopic,
} from "../liveApi";
import { signalHypothesisInput, type SignalBetForm } from "../signalHypothesis";
import { signalSources } from "../signalSources";
import { GoalSelect } from "./GoalSelect";
import { liveTopicName } from "./LiveOverview";

const FIELDS = ["title", "problem", "hypothesis", "grounds", "method", "criterion"] as const;
const HORIZONS: RadarBet["horizon"][] = ["0-1", "1-3", "3+"];

/** Отказ модели не оставляет форму пустой: основания и индикаторы собирает код радара. */
export function SignalHypothesisDialog({
  detail,
  candidate,
  query,
  topic,
  horizon,
  compact = false,
}: {
  detail: LiveDetail;
  candidate: LiveTopic;
  query: string;
  topic?: string;
  horizon?: RadarBet["horizon"];
  compact?: boolean;
}) {
  const { t } = useTranslation("trend");
  const name = liveTopicName(candidate, t);
  const [open, setOpen] = useState(false);
  const [brief, setBrief] = useState<LiveSignalBrief | null>(null);
  const [writing, setWriting] = useState(false);
  const [failed, setFailed] = useState(false);
  const [form, setForm] = useState<SignalBetForm>(() => ({
    title: name,
    problem: "",
    hypothesis: "",
    grounds: "",
    method: "",
    criterion: "",
  }));
  const [bet, setBet] = useState<RadarBet | null>(null);
  // Подсказка по смыслу действует только поверх «без привязки» и выбор из тезиса не перебивает.
  const [params] = useSearchParams();
  const [kpiId, setKpiId] = useState(() => params.get("thesis") || "none");
  const [busy, setBusy] = useState(false);
  const [createdId, setCreatedId] = useState("");
  const sources = signalSources(detail, candidate);

  const openDialog = async () => {
    setOpen(true);
    if (brief || writing) return;
    setWriting(true);
    setFailed(false);
    try {
      const written = await writeSignalBrief({
        query,
        name,
        candidate,
        sources: sources.map(({ paper }) => ({
          id: paper.id,
          title: paper.title,
          date: paper.date,
        })),
      });
      setBrief(written);
      setForm((current) => ({
        title: written.title || current.title,
        problem: written.problem,
        hypothesis: written.hypothesis,
        grounds: written.grounds,
        method: written.method,
        criterion: written.criterion,
      }));
      setBet({ ...written.bet, horizon: horizon ?? written.bet.horizon });
    } catch {
      setFailed(true);
    } finally {
      setWriting(false);
    }
  };
  const ready = form.title.trim() !== "" && form.hypothesis.trim() !== "";
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (busy || !ready) return;
    setBusy(true);
    try {
      const result = await createHypothesis(
        signalHypothesisInput({
          detail,
          candidate,
          query,
          topic,
          form,
          bet,
          model: brief?.model,
          kpiId: kpiId === "none" ? undefined : kpiId,
          t,
        }),
      );
      setCreatedId(result.id);
      toast.success(t("transfer.created"));
    } catch {
      toast.error(t("transfer.failed"));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Button variant="outline" size={compact ? "sm" : "default"} onClick={() => void openDialog()}>
        <Plus aria-hidden />
        {t("transfer.open")}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!busy) setOpen(value);
        }}
      >
        <DialogContent className="max-h-[85dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{t("transfer.title")}</DialogTitle>
            <DialogDescription>{t("transfer.description")}</DialogDescription>
          </DialogHeader>
          {createdId ? (
            <div className="space-y-4">
              <Notice tone="ok">{t("transfer.created")}</Notice>
              <Button asChild>
                <a href={`/hypotheses/${encodeURIComponent(createdId)}`}>
                  {t("transfer.openCreated")}
                  <ArrowUpRight aria-hidden />
                </a>
              </Button>
            </div>
          ) : writing ? (
            <output className="flex items-center gap-3 py-6 text-sm text-muted-foreground">
              <span aria-hidden="true">
                <Spinner />
              </span>
              {t("transfer.collecting")}
            </output>
          ) : (
            <form onSubmit={(event) => void submit(event)} className="space-y-4">
              {(failed || brief?.gap) && <Notice tone="warn">{t("transfer.briefFailed")}</Notice>}
              {FIELDS.map((field) => {
                const id = `signal-bet-${field}`;
                const control = {
                  id,
                  value: form[field],
                  onChange: (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) =>
                    setForm({ ...form, [field]: event.target.value }),
                  disabled: busy,
                  required: field === "title" || field === "hypothesis",
                  "aria-describedby": `${id}-hint`,
                };
                return (
                  <div key={field} className="space-y-1">
                    <Label htmlFor={id}>{t(`technology.implementation.fields.${field}`)}</Label>
                    <p id={`${id}-hint`} className="text-xs text-muted-foreground">
                      {t(`technology.implementation.hints.${field}`)}
                    </p>
                    {field === "title" ? (
                      <Input {...control} />
                    ) : (
                      <Textarea {...control} rows={field === "grounds" ? 5 : 3} />
                    )}
                  </div>
                );
              })}
              {bet && (
                <>
                  <div className="space-y-1">
                    <Label htmlFor="signal-bet-horizon">{t("transfer.horizon")}</Label>
                    <Select
                      value={bet.horizon}
                      onValueChange={(value) =>
                        setBet({ ...bet, horizon: value as RadarBet["horizon"] })
                      }
                      disabled={busy}
                    >
                      <SelectTrigger id="signal-bet-horizon">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {HORIZONS.map((value) => (
                          <SelectItem key={value} value={value}>
                            {t(`executive.horizons.${value}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <p className="text-sm">
                    <span className="text-muted-foreground">{t("transfer.stageNow")} </span>
                    {t(`transfer.stages.${bet.stage_code}`, {
                      defaultValue: t("transfer.stages.0"),
                    })}
                  </p>
                  <div className="space-y-1">
                    <Label htmlFor="signal-bet-trigger">{t("transfer.trigger")}</Label>
                    <p id="signal-bet-trigger-hint" className="text-xs text-muted-foreground">
                      {t("transfer.triggerHint")}
                    </p>
                    <Textarea
                      id="signal-bet-trigger"
                      aria-describedby="signal-bet-trigger-hint"
                      value={bet.next_stage_trigger}
                      onChange={(event) =>
                        setBet({ ...bet, next_stage_trigger: event.target.value })
                      }
                      disabled={busy}
                      rows={2}
                    />
                  </div>
                  <div className="space-y-1">
                    <p className="text-sm font-medium">{t("transfer.indicators")}</p>
                    <p className="text-xs text-muted-foreground">{t("transfer.indicatorsHint")}</p>
                    <ul className="list-disc space-y-0.5 pl-5 text-sm">
                      {bet.indicators.map((indicator) => (
                        <li key={indicator.label}>{indicator.label}</li>
                      ))}
                    </ul>
                  </div>
                </>
              )}
              <GoalSelect
                id="signal-hypothesis-goal"
                value={kpiId}
                onChange={setKpiId}
                disabled={busy}
                signal={[name, candidate.card?.summary].filter(Boolean).join(". ")}
              />
              <p className="text-sm text-muted-foreground">
                {t("transfer.preserved", { count: sources.length })}
              </p>
              <DialogFooter>
                <Button variant="outline" onClick={() => setOpen(false)} disabled={busy}>
                  {t("transfer.cancel")}
                </Button>
                <Button type="submit" disabled={busy || !ready}>
                  {t(busy ? "transfer.saving" : "transfer.confirm")}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
