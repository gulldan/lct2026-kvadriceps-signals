import { useTranslation } from "react-i18next";
import { formatNumber } from "@/shared/lib/format";
import { researchCategoryLabel, researchTaxonomy } from "@/shared/lib/researchTaxonomy";
import { DonutChart } from "@/shared/ui/DonutChart";
import { Disclosure } from "@/shared/ui/Disclosure";
import { Notice } from "@/shared/ui/Notice";
import { Section } from "@/shared/ui/Section";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/Table";
import type { AdminDocumentsStats } from "../api";

const COLORS = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
];

const INVESTOR_RESOURCES = [
  ["Y Combinator · Requests for Startups", "https://www.ycombinator.com/rfs"],
  ["a16z · American Dynamism", "https://a16z.com/american-dynamism/"],
  ["Breakthrough Energy", "https://www.breakthroughenergy.org/programs/"],
  ["SOSV", "https://sosv.com/"],
  ["AgFunder", "https://agfunder.com/"],
] as const;

function corpusShare(count: number, total: number): string {
  const percent = total > 0 ? (count / total) * 100 : 0;
  const belowPrecision = percent > 0 && percent < 0.1;
  return `${belowPrecision ? "<\u00a0" : ""}${formatNumber(belowPrecision ? 0.1 : percent, {
    maximumFractionDigits: 1,
  })}\u00a0%`;
}

export function TopicDistribution({ data }: { data: AdminDocumentsStats }) {
  const { t, i18n } = useTranslation("admin");
  const language = i18n.language;
  const distribution = data.topic_distribution;
  const currentCatalog = distribution?.method === researchTaxonomy.method;
  const topics =
    distribution?.topics.filter((topic) => topic.count > 0).toSorted((a, b) => b.count - a.count) ??
    [];
  const name = (topic: (typeof topics)[number]) => {
    const category =
      currentCatalog && researchTaxonomy.categories.find((item) => item.id === topic.id);
    return category
      ? researchCategoryLabel(category, language)
      : t(`dashboard.topics.domains.${topic.id}`, { defaultValue: topic.label });
  };
  const other = topics.slice(COLORS.length);
  const items = topics.slice(0, COLORS.length).map((topic, index) => ({
    key: topic.id,
    label: name(topic),
    value: topic.count,
    color: COLORS[index]!,
  }));
  if (other.length > 0)
    items.push({
      key: "other",
      label: t("dashboard.topics.other", { count: other.length }),
      value: other.reduce((sum, topic) => sum + topic.count, 0),
      color: "var(--foreground)",
    });
  if (distribution?.unclassified_documents)
    items.push({
      key: "unclassified",
      label: t("dashboard.topics.unclassified"),
      value: distribution.unclassified_documents,
      color: "var(--category-unassigned)",
    });
  // Ноль достоверен только при совпадающей версии: старый сервер знает другой набор категорий.
  const rows = currentCatalog
    ? researchTaxonomy.categories.map((category) => ({
        id: category.id,
        label: researchCategoryLabel(category, language),
        count: topics.find((topic) => topic.id === category.id)?.count ?? 0,
        category,
      }))
    : topics.map((topic) => ({ ...topic, label: name(topic), category: undefined }));
  return (
    <Section
      title={t("dashboard.topics.title")}
      description={t("dashboard.topics.description")}
      className="mt-7 border-t pt-5"
    >
      {!distribution || items.length === 0 ? (
        <Notice tone="muted">{t("dashboard.topics.unavailable")}</Notice>
      ) : (
        <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          <DonutChart
            data={items}
            label={t("dashboard.topics.title")}
            valueLabel={t("dashboard.topics.documents")}
          />
          <div className="space-y-4">
            <p className="text-sm text-muted-foreground">
              {t("dashboard.topics.coverage", {
                classified: formatNumber(distribution.classified_documents),
                total: formatNumber(data.total),
                count: topics.length,
              })}
            </p>
            {currentCatalog && (
              <p className="text-sm">
                <a
                  href={researchTaxonomy.source.url}
                  target="_blank"
                  rel="noreferrer"
                  className="text-brand underline underline-offset-4"
                >
                  {t("dashboard.topics.taxonomy")}
                </a>
              </p>
            )}
            <p className="text-sm text-muted-foreground">
              {t(
                currentCatalog || distribution.method === "title_rules_v1"
                  ? "dashboard.topics.titleRules"
                  : "dashboard.topics.methodUnknown",
              )}
            </p>
            {currentCatalog && (
              <p className="text-sm text-muted-foreground">{t("dashboard.topics.investorHint")}</p>
            )}
            <Disclosure title={t("dashboard.topics.all", { count: rows.length })}>
              {currentCatalog && (
                <p className="mb-3 text-xs text-muted-foreground">
                  {t("dashboard.topics.partition")}
                </p>
              )}
              <Table caption={t("dashboard.topics.all", { count: rows.length })}>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t("dashboard.topics.domain")}</TableHead>
                    <TableHead className="text-right">{t("dashboard.topics.documents")}</TableHead>
                    <TableHead className="text-right">{t("dashboard.topics.share")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((topic) => (
                    <TableRow key={topic.id}>
                      <TableCell>
                        {topic.label}
                        {topic.category && (
                          <p className="mt-1 text-xs text-muted-foreground">
                            {t(`dashboard.topics.basis.${topic.category.basis}`, {
                              defaultValue: t("dashboard.topics.basis.research_domain"),
                            })}
                            {topic.category.parent_ids.length > 0 &&
                              ` · ${topic.category.parent_ids
                                .map((id) => {
                                  const parent = researchTaxonomy.categories.find(
                                    (category) => category.id === id,
                                  )!;
                                  return researchCategoryLabel(parent, language);
                                })
                                .join(", ")}`}
                          </p>
                        )}
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        {formatNumber(topic.count)}
                      </TableCell>
                      <TableCell className="text-right font-mono whitespace-nowrap tabular-nums">
                        {corpusShare(topic.count, data.total)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Disclosure>
            {currentCatalog && (
              <Disclosure title={t("dashboard.topics.marketTitle")}>
                <p className="text-sm text-muted-foreground">{t("dashboard.topics.marketBody")}</p>
                <ul className="mt-3 space-y-2 text-sm">
                  {INVESTOR_RESOURCES.map(([label, href]) => (
                    <li key={href}>
                      <a
                        href={href}
                        target="_blank"
                        rel="noreferrer"
                        className="text-brand underline underline-offset-4"
                      >
                        {label}
                      </a>
                    </li>
                  ))}
                </ul>
              </Disclosure>
            )}
          </div>
        </div>
      )}
    </Section>
  );
}
