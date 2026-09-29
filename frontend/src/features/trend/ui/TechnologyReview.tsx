import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { Undo2 } from "lucide-react";
import { ApiError } from "@/shared/api/client";
import { useAuth } from "@/features/auth";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Chip } from "@/shared/ui/Chip";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/shared/ui/Dialog";
import { FieldRow } from "@/shared/ui/Fields";
import { Input } from "@/shared/ui/Input";
import { Notice } from "@/shared/ui/Notice";
import { SearchField } from "@/shared/ui/SearchField";
import { Section } from "@/shared/ui/Section";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/Select";
import { Textarea } from "@/shared/ui/Textarea";
import { formatDateTime } from "@/shared/lib/format";
import { searchTechnologies } from "../catalogueApi";
import {
  getReviews,
  submitReview,
  type Review,
  type ReviewInput,
  type Technology,
} from "../technologyApi";
import { ROLES, VALIDATION_STATUSES, known } from "../technologyLabels";

const OPERATIONS = [
  "rename",
  "describe",
  "confirm",
  "set_role",
  "set_dependency",
  "merge",
  "split",
] as const;
type FormOperation = (typeof OPERATIONS)[number];
const VALUE_KEYS = [
  "canonical_name",
  "description",
  "validation_status",
  "mention_id",
  "role",
  "depends_on_mention_id",
  "same_as",
  "same_as_name",
  "mention_ids",
] as const;

export function TechnologyReview({
  technology,
  onChanged,
}: {
  technology: Technology;
  onChanged: () => void;
}) {
  const { t } = useTranslation("trend");
  const { auth } = useAuth();
  const [reviews, setReviews] = useState<Review[] | null>(null);
  const [open, setOpen] = useState(false);
  const [saved, setSaved] = useState("");
  const [failure, setFailure] = useState("");
  const [version, setVersion] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    getReviews(technology.technology_id, controller.signal)
      .then((result) => {
        if (!controller.signal.aborted) setReviews(result.reviews);
      })
      .catch(() => {
        if (!controller.signal.aborted) setReviews([]);
      });
    return () => controller.abort();
  }, [technology.technology_id, version]);

  const send = async (input: ReviewInput) => {
    setFailure("");
    try {
      await submitReview(technology.technology_id, input);
      setSaved(t("review.saved"));
      setOpen(false);
      setVersion((v) => v + 1);
      onChanged();
      return true;
    } catch (error: unknown) {
      setFailure(reviewFailure(error, t));
      return false;
    }
  };
  const reverted = new Set((reviews ?? []).map((review) => review.reverts_id).filter(Boolean));
  const mentionLabel = (id: string) => {
    const mention = technology.mentions.find((item) => item.mention_id === id);
    if (!mention) return id;
    const role = known(ROLES, mention.role) ?? "unknown";
    return `${mention.source_title || t("technology.sources.untitled")} — ${t(`technology.roles.${role}`)}`;
  };

  return (
    <Section
      title={t("review.title")}
      description={t("review.intro")}
      action={
        <Button variant="outline" onClick={() => setOpen(true)}>
          {t("review.add")}
        </Button>
      }
    >
      {saved && (
        <output className="block">
          <Notice tone="ok">{saved}</Notice>
        </output>
      )}
      {failure && !open && (
        <Notice tone="risk" role="alert">
          {failure}
        </Notice>
      )}
      {reviews && reviews.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("review.empty")}</p>
      ) : (
        <ol className="divide-y">
          {(reviews ?? []).map((review) => (
            <li key={review.id} className="space-y-2 py-4 text-sm first:pt-0">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-medium">{t(`review.ops.${review.operation}`)}</span>
                {review.reverts_id && <Badge variant="muted">{t("review.ops.revert")}</Badge>}
                {reverted.has(review.id) && (
                  <Badge variant="warn">{t("review.history.reverted")}</Badge>
                )}
                <span className="text-xs text-muted-foreground">
                  {t("review.history.by", {
                    author:
                      review.author_id === auth?.user.id
                        ? t("review.history.you")
                        : review.author_id,
                  })}
                  {" · "}
                  {formatDateTime(review.created_at)}
                </span>
              </div>
              <p className="max-w-prose leading-relaxed">{review.reason}</p>
              <div className="grid gap-4 sm:grid-cols-2">
                <ValueList
                  title={t("review.history.previous")}
                  value={review.previous_value}
                  label={mentionLabel}
                />
                <ValueList
                  title={t("review.history.next")}
                  value={review.new_value}
                  label={mentionLabel}
                />
              </div>
              {!review.reverts_id && !reverted.has(review.id) && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() =>
                    void send({
                      operation: "revert",
                      reason: t("review.history.revertReason", { id: review.id.slice(0, 8) }),
                      previous_value: review.new_value,
                      new_value: review.previous_value,
                      reverts_id: review.id,
                    })
                  }
                >
                  <Undo2 aria-hidden />
                  {t("review.history.revert")}
                </Button>
              )}
            </li>
          ))}
        </ol>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("review.add")}</DialogTitle>
            <DialogDescription>{t("review.intro")}</DialogDescription>
          </DialogHeader>
          <ReviewForm
            technology={technology}
            failure={failure}
            onCancel={() => setOpen(false)}
            onSubmit={send}
            mentionLabel={mentionLabel}
          />
        </DialogContent>
      </Dialog>
    </Section>
  );
}

function ValueList({
  title,
  value,
  label,
}: {
  title: string;
  value: Record<string, unknown>;
  label: (id: string) => string;
}) {
  const { t } = useTranslation("trend");
  const entries = Object.entries(value);
  return (
    <div>
      <p className="text-xs text-muted-foreground">{title}</p>
      {entries.length === 0 ? (
        <p className="text-muted-foreground">{t("review.history.empty")}</p>
      ) : (
        <dl className="mt-1 space-y-1">
          {entries.map(([key, raw]) => {
            const name = known(VALUE_KEYS, key);
            const shown = Array.isArray(raw)
              ? raw
                  .map((item) => (key === "mention_ids" ? label(String(item)) : String(item)))
                  .join(", ")
              : raw === null || raw === undefined || raw === ""
                ? t("review.history.empty")
                : key === "mention_id" || key === "depends_on_mention_id"
                  ? label(String(raw))
                  : String(raw);
            return (
              <div key={key} className="flex flex-wrap gap-x-2">
                <dt className="text-muted-foreground">{name ? t(`review.keys.${name}`) : key}:</dt>
                <dd className="wrap-anywhere">{shown}</dd>
              </div>
            );
          })}
        </dl>
      )}
    </div>
  );
}

function reviewFailure(
  error: unknown,
  t: (
    key:
      | "review.errors.forbidden"
      | "review.errors.conflict"
      | "review.errors.invalid"
      | "review.errors.generic",
  ) => string,
) {
  if (!(error instanceof ApiError)) return t("review.errors.generic");
  if (error.status === 403) return t("review.errors.forbidden");
  if (error.status === 409) return t("review.errors.conflict");
  if (error.status === 400) return t("review.errors.invalid");
  return t("review.errors.generic");
}

function ReviewForm({
  technology,
  failure,
  onCancel,
  onSubmit,
  mentionLabel,
}: {
  technology: Technology;
  failure: string;
  onCancel: () => void;
  onSubmit: (input: ReviewInput) => Promise<boolean>;
  mentionLabel: (id: string) => string;
}) {
  const { t } = useTranslation("trend");
  const [operation, setOperation] = useState<FormOperation>("rename");
  const [name, setName] = useState(technology.canonical_name);
  const [description, setDescription] = useState(technology.description);
  const [status, setStatus] = useState(
    known(VALIDATION_STATUSES, technology.validation_status) ?? "proposed",
  );
  const [mentionId, setMentionId] = useState(technology.mentions[0]?.mention_id ?? "");
  const [role, setRole] = useState<(typeof ROLES)[number]>("primary");
  const [dependsOn, setDependsOn] = useState("");
  const [targetQuery, setTargetQuery] = useState("");
  const [targets, setTargets] = useState<{ id: string; name: string }[] | null>(null);
  const [target, setTarget] = useState<{ id: string; name: string } | null>(null);
  const [splitName, setSplitName] = useState("");
  const [splitMentions, setSplitMentions] = useState<string[]>([]);
  const [reason, setReason] = useState("");
  const [problem, setProblem] = useState("");
  const [saving, setSaving] = useState(false);
  const mention = technology.mentions.find((item) => item.mention_id === mentionId);

  const build = (): ReviewInput | string => {
    if (reason.trim().length < 3) return t("review.errors.reasonRequired");
    switch (operation) {
      case "rename":
        if (!name.trim() || name.trim() === technology.canonical_name)
          return t("review.errors.valueRequired");
        return {
          operation,
          reason,
          previous_value: { canonical_name: technology.canonical_name },
          new_value: { canonical_name: name.trim() },
        };
      case "describe":
        if (!description.trim() || description.trim() === technology.description)
          return t("review.errors.valueRequired");
        return {
          operation,
          reason,
          previous_value: { description: technology.description },
          new_value: { description: description.trim() },
        };
      case "confirm":
        if (status === technology.validation_status) return t("review.errors.valueRequired");
        return {
          operation,
          reason,
          previous_value: { validation_status: technology.validation_status },
          new_value: { validation_status: status },
        };
      case "set_role":
        if (!mention || role === mention.role) return t("review.errors.valueRequired");
        return {
          operation,
          reason,
          previous_value: { mention_id: mention.mention_id, role: mention.role },
          new_value: { mention_id: mention.mention_id, role },
        };
      case "set_dependency":
        if (!mention || !dependsOn || dependsOn === mention.mention_id)
          return t("review.errors.valueRequired");
        return {
          operation,
          reason,
          previous_value: { mention_id: mention.mention_id, depends_on_mention_id: null },
          new_value: { mention_id: mention.mention_id, depends_on_mention_id: dependsOn },
        };
      case "merge":
        if (!target) return t("review.errors.valueRequired");
        return {
          operation,
          reason,
          previous_value: { same_as: "" },
          new_value: { same_as: target.id, same_as_name: target.name },
        };
      case "split":
        if (!splitName.trim() || splitMentions.length === 0)
          return t("review.errors.valueRequired");
        return {
          operation,
          reason,
          previous_value: { mention_ids: [] },
          new_value: { canonical_name: splitName.trim(), mention_ids: splitMentions },
        };
    }
  };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const input = build();
    if (typeof input === "string") {
      setProblem(input);
      return;
    }
    setProblem("");
    setSaving(true);
    await onSubmit(input);
    setSaving(false);
  };
  const findTargets = async () => {
    const page = await searchTechnologies({ q: targetQuery.trim(), limit: 8 }).catch(() => null);
    setTargets(
      (page?.technologies ?? [])
        .filter((hit) => hit.technology_id !== technology.technology_id)
        .map((hit) => ({ id: hit.technology_id, name: hit.canonical_name })),
    );
  };
  const mentionSelect = (
    id: string,
    value: string,
    onChange: (next: string) => void,
    label: string,
  ) => (
    <FieldRow id={id} label={label}>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {technology.mentions.map((item) => (
            <SelectItem key={item.mention_id} value={item.mention_id}>
              {mentionLabel(item.mention_id)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </FieldRow>
  );
  return (
    <form onSubmit={(event) => void submit(event)} className="space-y-4">
      <FieldRow id="review-operation" label={t("review.operation")}>
        <Select value={operation} onValueChange={(value) => setOperation(value as FormOperation)}>
          <SelectTrigger id="review-operation" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {OPERATIONS.map((op) => (
              <SelectItem key={op} value={op}>
                {t(`review.ops.${op}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </FieldRow>
      {operation === "rename" && (
        <FieldRow id="review-name" label={t("review.fields.name")}>
          <Input id="review-name" value={name} onChange={(event) => setName(event.target.value)} />
        </FieldRow>
      )}
      {operation === "describe" && (
        <FieldRow id="review-description" label={t("review.fields.description")}>
          <Textarea
            id="review-description"
            rows={4}
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </FieldRow>
      )}
      {operation === "confirm" && (
        <FieldRow id="review-status" label={t("review.fields.status")}>
          <Select value={status} onValueChange={(value) => setStatus(value as typeof status)}>
            <SelectTrigger id="review-status" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {VALIDATION_STATUSES.map((item) => (
                <SelectItem key={item} value={item}>
                  {t(`technology.status.${item}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldRow>
      )}
      {(operation === "set_role" || operation === "set_dependency") &&
        mentionSelect("review-mention", mentionId, setMentionId, t("review.fields.mention"))}
      {operation === "set_role" && (
        <FieldRow id="review-role" label={t("review.fields.role")}>
          <Select value={role} onValueChange={(value) => setRole(value as typeof role)}>
            <SelectTrigger id="review-role" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ROLES.map((item) => (
                <SelectItem key={item} value={item}>
                  {t(`technology.roles.${item}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </FieldRow>
      )}
      {operation === "set_dependency" &&
        mentionSelect("review-depends", dependsOn, setDependsOn, t("review.fields.dependsOn"))}
      {operation === "merge" && (
        <FieldRow
          id="review-target"
          label={t("review.fields.target")}
          hint={t("review.fields.targetHint")}
        >
          <div className="w-full space-y-2">
            <div className="flex gap-2">
              <SearchField
                className="min-w-0 flex-1"
                value={targetQuery}
                onChange={setTargetQuery}
                ariaLabel={t("review.fields.search")}
                placeholder={t("review.fields.search")}
              />
              <Button
                type="button"
                variant="outline"
                onClick={() => void findTargets()}
                disabled={!targetQuery.trim()}
              >
                {t("review.fields.search")}
              </Button>
            </div>
            {targets && targets.length === 0 && (
              <p className="text-xs text-muted-foreground">{t("review.fields.noMatches")}</p>
            )}
            {targets && targets.length > 0 && (
              <fieldset className="flex flex-wrap gap-2">
                <legend className="sr-only">{t("review.fields.target")}</legend>
                {targets.map((hit) => (
                  <Chip
                    key={hit.id}
                    aria-pressed={target?.id === hit.id}
                    onClick={() => setTarget(hit)}
                  >
                    {hit.name}
                  </Chip>
                ))}
              </fieldset>
            )}
          </div>
        </FieldRow>
      )}
      {operation === "split" && (
        <>
          <FieldRow id="review-split-name" label={t("review.fields.splitName")}>
            <Input
              id="review-split-name"
              value={splitName}
              onChange={(event) => setSplitName(event.target.value)}
            />
          </FieldRow>
          <fieldset className="space-y-2">
            <legend className="text-sm font-medium">{t("review.fields.splitMentions")}</legend>
            <div className="flex flex-wrap gap-2">
              {technology.mentions.map((item) => (
                <Chip
                  key={item.mention_id}
                  aria-pressed={splitMentions.includes(item.mention_id)}
                  onClick={() =>
                    setSplitMentions((current) =>
                      current.includes(item.mention_id)
                        ? current.filter((id) => id !== item.mention_id)
                        : [...current, item.mention_id],
                    )
                  }
                >
                  {mentionLabel(item.mention_id)}
                </Chip>
              ))}
            </div>
          </fieldset>
        </>
      )}
      <FieldRow
        id="review-reason"
        label={t("review.fields.reason")}
        hint={t("review.fields.reasonHint")}
      >
        <Textarea
          id="review-reason"
          rows={3}
          value={reason}
          onChange={(event) => setReason(event.target.value)}
        />
      </FieldRow>
      {(problem || failure) && (
        <p role="alert" className="text-sm text-destructive">
          {problem || failure}
        </p>
      )}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onCancel}>
          {t("actions.cancel", { ns: "common" })}
        </Button>
        <Button type="submit" disabled={saving}>
          {saving ? t("review.saving") : t("review.submit")}
        </Button>
      </DialogFooter>
    </form>
  );
}
