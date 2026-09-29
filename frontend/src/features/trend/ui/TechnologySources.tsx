import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react";
import { sourcePath } from "@/features/document";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { formatDate } from "@/shared/lib/format";
import { EXTRACTION_STATUSES, ROLES, known } from "../technologyLabels";
import type { Mention } from "../technologyApi";

export interface SourceGroup {
  id: string;
  title: string;
  date: string;
  mentions: Mention[];
}

export function groupBySource(mentions: Mention[]): SourceGroup[] {
  const groups = new Map<string, SourceGroup>();
  for (const mention of mentions) {
    const id = mention.source_revision_id || mention.mention_id;
    const group = groups.get(id) ?? {
      id,
      title: mention.source_title ?? "",
      date: mention.source_published_at ?? "",
      mentions: [],
    };
    group.mentions.push(mention);
    groups.set(id, group);
  }
  return [...groups.values()].toSorted((a, b) =>
    a.date && b.date ? b.date.localeCompare(a.date) : a.date ? -1 : b.date ? 1 : 0,
  );
}

function RoleBadge({ role }: { role: string }) {
  const { t } = useTranslation("trend");
  const key = known(ROLES, role) ?? "unknown";
  return (
    <Badge variant={key === "primary" ? "brand" : key === "unknown" ? "muted" : "outline"}>
      {t(`technology.roles.${key}`)}
    </Badge>
  );
}

export function TechnologySources({
  groups,
  returnTo,
}: {
  groups: SourceGroup[];
  returnTo: string;
}) {
  const { t } = useTranslation("trend");
  return (
    <ul className="divide-y">
      {groups.map((group) => (
        <li key={group.id} className="space-y-3 py-4 first:pt-0">
          <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
            <Button variant="link" size="inline" asChild className="font-medium">
              <Link to={sourcePath(group.id, undefined, undefined, { returnTo })}>
                {group.title || t("technology.sources.untitled")}
              </Link>
            </Button>
            <span className="text-xs text-muted-foreground">
              {group.date ? formatDate(group.date) : t("technology.sources.dateUnknown")}
            </span>
          </div>
          {group.mentions.map((mention) => {
            const status = known(EXTRACTION_STATUSES, mention.extraction_status);
            return (
              <div key={mention.mention_id} className="space-y-2">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <RoleBadge role={mention.role} />
                  {mention.name && <span className="text-muted-foreground">{mention.name}</span>}
                  {status && status !== "complete" && (
                    <Badge variant="warn">{t(`technology.extraction.${status}`)}</Badge>
                  )}
                </div>
                {mention.evidence.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    {t("technology.sources.noQuotes")}
                  </p>
                ) : (
                  <ul className="space-y-2">
                    {mention.evidence.map((quote) => (
                      <li key={quote.evidence_id} className="text-sm leading-relaxed">
                        <mark className="rounded-sm bg-brand-wash px-0.5">{quote.quote}</mark>{" "}
                        <Button variant="link" size="inline" asChild>
                          <Link
                            aria-label={t("technology.sources.quote")}
                            to={sourcePath(group.id, undefined, undefined, {
                              anchor: {
                                start: quote.locator.start_char,
                                end: quote.locator.end_char,
                                textHash: quote.locator.snapshot_sha256 ?? "",
                              },
                              returnTo,
                            })}
                          >
                            <ExternalLink aria-hidden />
                            {quote.locator.section}
                            {quote.locator.page ? `, ${quote.locator.page}` : ""}
                          </Link>
                        </Button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            );
          })}
        </li>
      ))}
    </ul>
  );
}
