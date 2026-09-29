import { useTranslation } from "react-i18next";
import { Check, CircleHelp, ExternalLink } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Notice } from "@/shared/ui/Notice";
import { formatNumber, formatPercent } from "@/shared/lib/format";
import type { StudyTopic, TrendStudy } from "../studyApi";
import { StudySources } from "./StudySources";

const PAPERS = [
  {
    name: "Rotolo, Hicks & Martin · What Is an Emerging Technology? (2015)",
    url: "https://arxiv.org/abs/1503.00673",
  },
  {
    name: "Edge et al. · From Local to Global: A Graph RAG Approach (2024)",
    url: "https://arxiv.org/abs/2404.16130",
  },
  {
    name: "Lewis et al. · Retrieval-Augmented Generation (2020)",
    url: "https://arxiv.org/abs/2005.11401",
  },
];

const number = (n: number) => formatNumber(n, { maximumFractionDigits: 2 });
const percent = (n: number | null) => (n === null ? "—" : formatPercent(n, 2));

export function StudyExplanation({ study, topic }: { study: TrendStudy; topic: StudyTopic }) {
  const { t } = useTranslation("trend");
  const growth = topic.growth;
  return (
    <div className="space-y-6 text-sm leading-6">
      {!topic.reviewed && <Notice>{t("study.explanation.lexical")}</Notice>}
      {topic.screening === "titles" && <Notice>{t("study.explanation.titles")}</Notice>}
      {topic.id === "agentic" && <p>{t("study.explanation.agentic")}</p>}
      {topic.id === "graph" && (
        <>
          <p>{t("study.explanation.graph")}</p>
          <figure aria-label={t("study.explanation.diagram")} className="@container space-y-3">
            <svg viewBox="0 0 560 116" className="h-28 w-full" aria-hidden>
              {[0, 1, 2].map((n) => (
                <g key={n} transform={`translate(${22 + n * 18},${15 + n * 12})`}>
                  <rect
                    width="54"
                    height="58"
                    rx="4"
                    fill="var(--color-card)"
                    stroke="var(--color-border)"
                  />
                  <path
                    d="M10 15H44 M10 25H38 M10 35H44 M10 45H32"
                    fill="none"
                    stroke="var(--color-muted-foreground)"
                  />
                </g>
              ))}
              <path
                d="M127 58H188 L180 52 M188 58L180 64 M364 58H426 L418 52 M426 58L418 64"
                stroke="var(--color-muted-foreground)"
                fill="none"
              />
              <path
                d="M225 30L265 58L312 25L338 65L293 95L265 58L228 92L225 30 M265 58L338 65"
                stroke="var(--color-border)"
                fill="none"
              />
              <path
                d="M225 30L265 58L293 95"
                stroke="var(--color-brand)"
                strokeWidth="3"
                fill="none"
              />
              {[
                [225, 30],
                [265, 58],
                [293, 95],
              ].map(([x, y]) => (
                <circle key={x} cx={x} cy={y} r="8" fill="var(--color-brand)" />
              ))}
              {[
                [312, 25],
                [338, 65],
                [228, 92],
              ].map(([x, y]) => (
                <circle key={x} cx={x} cy={y} r="6" fill="var(--color-muted-foreground)" />
              ))}
              <rect
                x="444"
                y="22"
                width="86"
                height="74"
                rx="4"
                fill="var(--color-brand-wash)"
                stroke="var(--color-brand-border)"
              />
              <path
                d="M457 39H515 M457 51H504 M457 63H514 M457 75H492"
                stroke="var(--color-brand)"
              />
            </svg>
            <figcaption className="grid grid-cols-1 gap-3 text-xs leading-5 wrap-anywhere text-muted-foreground @min-[18rem]:grid-cols-3 @min-[18rem]:text-center">
              <span>1. {t("study.explanation.step1")}</span>
              <span>2. {t("study.explanation.step2")}</span>
              <span>3. {t("study.explanation.step3")}</span>
            </figcaption>
          </figure>
        </>
      )}
      {topic.reviewed && (
        <ul className="divide-y">
          {(["novelty", "coherence", "growth", "impact", "uncertainty"] as const).map(
            (criterion) => {
              const supported =
                criterion === "novelty"
                  ? ["new", "recent"].includes(topic.signals.novelty)
                  : criterion === "coherence" ||
                    (criterion === "growth" && topic.momentum === "up");
              const Icon = supported ? Check : CircleHelp;
              const key =
                criterion === "growth" && !supported
                  ? "growthUnconfirmed"
                  : criterion === "coherence" && topic.screening === "titles"
                    ? "coherenceTitles"
                    : criterion === "novelty" && !supported
                      ? "noveltyUnconfirmed"
                      : criterion === "novelty" && topic.id === "agentic"
                        ? "noveltyAgentic"
                        : criterion === "impact" && topic.id === "agentic"
                          ? "impactAgentic"
                          : criterion;
              return (
                <li key={criterion} className="flex items-start gap-3 py-3">
                  <Icon className="mt-1 size-4 shrink-0" aria-hidden />
                  <div>
                    <span className="font-medium">{t(`study.explanation.${key}Title`)}</span>
                    <p className="text-muted-foreground">
                      {t(`study.explanation.${key}`, {
                        count: topic.paper_ids.length,
                        years: topic.active_years,
                        groups: topic.groups,
                        year: topic.first_year,
                      })}
                    </p>
                  </div>
                </li>
              );
            },
          )}
        </ul>
      )}
      <section className="space-y-3 border-t pt-5" aria-label={t("study.explanation.calculation")}>
        <h3 className="font-medium">{t("study.explanation.calculation")}</h3>
        <div className="grid grid-cols-2 gap-4">
          {[growth.prior, growth.recent].map((row) => (
            <div key={row.period}>
              <p className="text-muted-foreground">{row.period}</p>
              <p className="mt-1 font-mono text-lg">
                {row.count} / {row.population}
              </p>
              <p className="text-muted-foreground">
                {t("study.explanation.shareValue", { value: percent(row.share) })}
              </p>
            </div>
          ))}
        </div>
        <p>
          {growth.delta_pp === null
            ? t("study.partialShort")
            : t("study.explanation.result", {
                prior: percent(growth.prior.share),
                recent: percent(growth.recent.share),
                delta: `${growth.delta_pp > 0 ? "+" : ""}${number(growth.delta_pp)}`,
              })}
        </p>
        <p className="text-muted-foreground">
          {t("study.explanation.ratio", {
            ratio: growth.share_ratio === null ? "—" : number(growth.share_ratio),
          })}
        </p>
        <p className="text-muted-foreground">{t("study.explanation.descriptive")}</p>
      </section>
      <Disclosure title={t("study.explanation.collection")}>
        <div className="space-y-3">
          <p>
            {t("study.explanation.population", {
              total: study.enumerated_papers,
              count: study.expected_papers,
            })}
          </p>
          <p>{t("study.explanation.dates")}</p>
          <p>{t("study.explanation.classification")}</p>
          <p>{t("study.explanation.safeguards")}</p>
          <Button variant="link" size="inline" asChild>
            <a
              href={`https://github.com/acl-org/acl-anthology/tree/${study.revision}/data/xml`}
              target="_blank"
              rel="noopener noreferrer"
            >
              {t("study.explanation.manifest")}
              <ExternalLink aria-hidden />
            </a>
          </Button>
          <p className="font-mono text-xs text-muted-foreground">
            {study.method} · {study.revision.slice(0, 12)}
          </p>
        </div>
      </Disclosure>
      <Disclosure title={t("study.explanation.science")}>
        <p className="mb-3">{t("study.explanation.scienceIntro")}</p>
        <ul className="space-y-3">
          {PAPERS.map((paper) => (
            <li key={paper.url}>
              <Button asChild variant="link" size="inline" className="text-left">
                <a href={paper.url} target="_blank" rel="noopener noreferrer">
                  {paper.name}
                  <ExternalLink aria-hidden />
                </a>
              </Button>
            </li>
          ))}
        </ul>
      </Disclosure>
      <StudySources study={study} topic={topic} milestones />
    </div>
  );
}
