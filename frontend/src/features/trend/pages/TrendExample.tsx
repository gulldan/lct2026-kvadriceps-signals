import { ArrowLeft, ArrowRight, ExternalLink } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/Button";
import { Notice } from "@/shared/ui/Notice";
import { ReadingContent } from "@/shared/ui/ReadingContent";
import { Section } from "@/shared/ui/Section";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader, PageToolbar, BackButton } from "@/shared/ui/PageHeader";
import { useReturnFocus } from "../useReturnFocus";
import { analysisRequestParams } from "../trendApi";

const PAPERS = [
  { key: "hyde", name: "HyDE", date: "2022-12-20", url: "https://arxiv.org/abs/2212.10496v1" },
  {
    key: "query2doc",
    name: "Query2doc",
    date: "2023-03-14",
    url: "https://arxiv.org/abs/2303.07678v1",
  },
] as const;

export function TrendExample() {
  const { t } = useTranslation("trend");
  const navigate = useNavigate();
  useReturnFocus(true);
  return (
    <PageLayout className="space-y-6">
      <PageToolbar>
        <BackButton asChild>
          <Link to="/trends?view=analysis#example-link">
            <ArrowLeft aria-hidden />
            {t("backRadar")}
          </Link>
        </BackButton>
      </PageToolbar>
      <PageHeader title={t("demo.title")} description={t("demo.provenance")} />
      <Notice tone="brand">{t("demo.status")}</Notice>
      <ReadingContent>
        <Section reading title={t("problem")}>
          <p>{t("demo.problem")}</p>
        </Section>
        <Section reading title={t("demo.approachTitle")}>
          <ol className="grid gap-5 sm:grid-cols-3">
            {(["query", "document", "retrieval"] as const).map((step, index) => (
              <li key={step} className="min-w-0 space-y-2 border-t pt-4">
                <h3 className="font-medium">
                  {index + 1}. {t(`demo.steps.${step}.title`)}
                </h3>
                <p className="text-muted-foreground">{t(`demo.steps.${step}.body`)}</p>
              </li>
            ))}
          </ol>
        </Section>
        <Section reading title={t("demo.evidenceTitle")}>
          <div className="divide-y">
            {PAPERS.map((paper) => (
              <section key={paper.key} className="space-y-3 py-5 first:pt-0 last:pb-0">
                <h3 className="flex flex-wrap items-baseline gap-x-3 gap-y-1 font-medium">
                  {paper.name}
                  <time dateTime={paper.date} className="text-sm font-normal text-muted-foreground">
                    {paper.date}
                  </time>
                </h3>
                <p>{t(`demo.${paper.key}`)}</p>
                <Button asChild variant="link" size="inline">
                  <a href={paper.url} target="_blank" rel="noopener noreferrer">
                    {t("demo.paperLink", { name: paper.name })}
                    <ExternalLink aria-hidden />
                  </a>
                </Button>
              </section>
            ))}
          </div>
        </Section>
        <Section reading title={t("demo.limitTitle")}>
          <p>{t("demo.limit")}</p>
          <Button asChild variant="link" size="inline">
            <Link id="method-link" to="/trends/method?return=example">
              {t("methodTitle")}
            </Link>
          </Button>
        </Section>
        <Section reading title={t("demo.nextTitle")} className="border-t pt-8">
          <p>{t("demo.next")}</p>
          <Button
            onClick={() =>
              navigate(
                `/trends?${analysisRequestParams({ query: "query expansion", start: "2022-01-01", cutoff: new Date().toISOString().slice(0, 10), mode: "as_of" })}`,
              )
            }
          >
            {t("demo.useQuery")}
            <ArrowRight aria-hidden />
          </Button>
          <p className="text-sm text-muted-foreground">{t("demo.useQueryHint")}</p>
        </Section>
      </ReadingContent>
    </PageLayout>
  );
}
