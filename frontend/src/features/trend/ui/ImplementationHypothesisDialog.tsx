import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowUpRight } from "lucide-react";
import { createHypothesis } from "@/features/hypothesis";
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
import { Spinner } from "@/shared/ui/Spinner";
import { Textarea } from "@/shared/ui/Textarea";
import { toast } from "@/shared/ui/Sonner";
import type { Technology, TechnologyBrief } from "../technologyApi";
import { GoalSelect } from "./GoalSelect";
import { useWriteBrief } from "./TechnologyBrief";

const FIELDS = ["title", "problem", "hypothesis", "grounds", "method", "criterion"] as const;
type Form = Record<(typeof FIELDS)[number], string>;

function fromBrief(brief: TechnologyBrief | null, name: string): Form {
  return {
    title: brief?.title || name,
    problem: brief?.problem ?? "",
    hypothesis: brief?.hypothesis ?? "",
    grounds: brief?.grounds ?? "",
    method: brief?.method ?? "",
    criterion: brief?.criterion ?? "",
  };
}

export function ImplementationHypothesisDialog({
  technology,
  brief,
  onBrief,
}: {
  technology: Technology;
  brief: TechnologyBrief | null;
  onBrief: (brief: TechnologyBrief) => void;
}) {
  const { t } = useTranslation("trend");
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState(() => fromBrief(brief, technology.canonical_name));
  const [kpiId, setKpiId] = useState("none");
  const [busy, setBusy] = useState(false);
  const [createdId, setCreatedId] = useState("");
  const writer = useWriteBrief(technology.technology_id, onBrief);
  useEffect(() => {
    if (brief) setForm(fromBrief(brief, technology.canonical_name));
  }, [brief, technology.canonical_name]);

  const openDialog = () => {
    setOpen(true);
    if (!brief && !writer.busy) void writer.write();
  };
  const ready = form.title.trim() !== "" && form.hypothesis.trim() !== "";
  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (busy || !ready) return;
    setBusy(true);
    try {
      const result = await createHypothesis({
        title: form.title.trim(),
        statement: form.hypothesis.trim(),
        rationale: form.grounds.trim(),
        method: "manual",
        status: "draft",
        source_type: "Каталог технологий",
        kpi_id: kpiId === "none" ? undefined : kpiId,
        measurable: false,
        generation: {
          kind: "technology",
          model: brief?.model,
          prompt_version: "technology-brief-v1",
        },
        detail: {
          problem_addressed: form.problem.trim(),
          verification: { method: form.method.trim(), success_criteria: form.criterion.trim() },
          technology: {
            id: technology.technology_id,
            name: technology.canonical_name,
            sources: brief?.sources,
          },
        },
      });
      setCreatedId(result.id);
      toast.success(t("technology.implementation.created"));
    } catch {
      toast.error(t("transfer.failed"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Button onClick={openDialog}>{t("technology.implementation.open")}</Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!busy) setOpen(value);
        }}
      >
        <DialogContent className="max-h-[85dvh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{t("technology.implementation.title")}</DialogTitle>
            <DialogDescription>{t("technology.implementation.description")}</DialogDescription>
          </DialogHeader>
          {createdId ? (
            <div className="space-y-4">
              <Notice tone="ok">{t("technology.implementation.created")}</Notice>
              <Button asChild>
                <a href={`/hypotheses/${encodeURIComponent(createdId)}`}>
                  {t("transfer.openCreated")}
                  <ArrowUpRight aria-hidden />
                </a>
              </Button>
            </div>
          ) : writer.busy ? (
            <output className="flex items-center gap-3 py-6 text-sm text-muted-foreground">
              <span aria-hidden="true">
                <Spinner />
              </span>
              {t("technology.implementation.collecting")}
            </output>
          ) : (
            <form onSubmit={(event) => void submit(event)} className="space-y-4">
              {writer.error && <Notice tone="warn">{t(`technology.brief.${writer.error}`)}</Notice>}
              {FIELDS.map((field) => {
                const id = `implementation-${field}`;
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
                      <Textarea {...control} rows={field === "hypothesis" ? 4 : 3} />
                    )}
                  </div>
                );
              })}
              <GoalSelect
                id="implementation-goal"
                value={kpiId}
                onChange={setKpiId}
                disabled={busy}
              />
              <Notice tone="muted">{t("technology.implementation.verification")}</Notice>
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
