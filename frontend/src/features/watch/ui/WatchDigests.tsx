import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { i18n } from "@/shared/i18n";
import { formatDate } from "@/shared/lib/format";
import { DropdownMenuSeparator } from "@/shared/ui/DropdownMenu";
import { Kicker } from "@/shared/ui/Kicker";
import { ScrollArea } from "@/shared/ui/ScrollArea";
import { watchLink, type Watch, type WatchDigest } from "../api";

type Shift = NonNullable<WatchDigest["stage"]>;

const stage = (value: Shift["from"]) => i18n.t(`trend:study.stage.${value}`);

function digestLines(digest: WatchDigest, t: TFunction<"task">) {
  const shift = (value: Shift) => ({ from: stage(value.from), to: stage(value.to) });
  const technologies = digest.technologies ?? [];
  return [
    ...(technologies.length
      ? [
          t("watch.digest.technologies", {
            count: technologies.length,
            names: technologies.join(", "),
          }),
        ]
      : []),
    ...(digest.stage ? [t("watch.digest.stage", shift(digest.stage))] : []),
    ...(digest.stages ?? []).map((row) =>
      t("watch.digest.techStage", { name: row.name ?? "", ...shift(row) }),
    ),
    ...(digest.sources ? [t("watch.digest.sources", { count: digest.sources })] : []),
    ...(digest.market ? [t("watch.digest.market", { count: digest.market })] : []),
    ...(digest.bet
      ? [
          t("watch.digest.bet", { status: t(`watch.betStatus.${digest.bet.status}`) }),
          ...digest.bet.changes,
        ]
      : []),
  ];
}

export function WatchDigests({
  watches,
  onOpen,
}: {
  watches: Watch[];
  onOpen: (watch: Watch) => void;
}) {
  const { t } = useTranslation("task");
  return (
    <>
      <Kicker className="px-2 pt-2 pb-1">{t("watch.title")}</Kicker>
      {/* Подписок до полусотни: без своей прокрутки они вытолкнули бы задачи за край окна. */}
      <ScrollArea aria-label={t("watch.title")} className="max-h-64">
        <ul>
          {watches.map((watch) => {
            const lines = watch.digest ? digestLines(watch.digest, t) : [];
            return (
              <li key={`${watch.kind}:${watch.target}`}>
                <Link
                  to={watchLink(watch)}
                  onClick={() => onOpen(watch)}
                  className="block rounded-md px-2 py-1.5 outline-none hover:bg-accent focus-visible:bg-accent"
                >
                  <span className="flex items-baseline justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-1.5 text-sm font-medium">
                      {lines.length > 0 && (
                        <span className="size-1.5 shrink-0 rounded-full bg-primary" aria-hidden />
                      )}
                      <span className="truncate">{watch.title}</span>
                    </span>
                    {lines.length > 0 && watch.digest_at && (
                      <span className="shrink-0 text-caption text-muted-foreground">
                        {formatDate(watch.digest_at)}
                      </span>
                    )}
                  </span>
                  {lines.length > 0 ? (
                    lines.map((line) => (
                      <span key={line} className="block text-xs text-muted-foreground">
                        {line}
                      </span>
                    ))
                  ) : (
                    <span className="block text-xs text-muted-foreground">{t("watch.quiet")}</span>
                  )}
                </Link>
              </li>
            );
          })}
        </ul>
      </ScrollArea>
      <DropdownMenuSeparator />
    </>
  );
}
