import { ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button } from "@/shared/ui/Button";
import { Section } from "@/shared/ui/Section";

export function TrendExamplePreview({ onRun }: { onRun: () => void }) {
  const { t } = useTranslation("trend");
  return (
    <Section
      reading
      id="example-heading"
      title={t("demo.previewTitle")}
      description={t("demo.previewIntro")}
      className="border-t pt-8"
      action={
        <div className="flex min-w-0 flex-wrap items-baseline gap-x-4 gap-y-2 text-sm leading-6 text-muted-foreground">
          <Button asChild variant="link" size="inline">
            <Link id="example-link" to="/trends/example">
              {t("demo.open")}
            </Link>
          </Button>
          <Button asChild variant="link" size="inline">
            <Link id="method-link" to="/trends/method">
              {t("methodTitle")}
            </Link>
          </Button>
        </div>
      }
    >
      <h3 className="font-display text-2xl text-balance">RAG · Retrieval-Augmented Generation</h3>
      <p className="max-w-3xl text-base leading-7">{t("demo.runHint")}</p>
      <Button id="demo-run" onClick={onRun}>
        {t("demo.run")}
        <ArrowRight aria-hidden />
      </Button>
    </Section>
  );
}
