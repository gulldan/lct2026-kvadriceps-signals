import { useTranslation } from "react-i18next";
import { ExternalLink } from "lucide-react";
import { Button } from "@/shared/ui/Button";
import { Disclosure } from "@/shared/ui/Disclosure";

export function SourceQuote({
  label,
  href,
  quote,
  note,
}: {
  label: string;
  href?: string;
  quote?: string;
  note?: string;
}) {
  const { t } = useTranslation("trend");
  return (
    <div className="min-w-0 text-caption text-muted-foreground">
      {href ? (
        <Button asChild variant="link" size="inline" className="text-left">
          <a href={href} target="_blank" rel="noopener noreferrer">
            {label}
            <ExternalLink aria-hidden />
          </a>
        </Button>
      ) : (
        <span>{label}</span>
      )}
      {quote && (
        <Disclosure
          title={t("report.quote")}
          className="border-t-0"
          summaryClassName="justify-start py-1 text-caption font-normal text-muted-foreground"
        >
          <blockquote className="border-l-2 border-border-strong pl-3 text-sm leading-6">
            «{quote.trim()}»
          </blockquote>
          {note && <p className="mt-1 pl-3.5">{note}</p>}
        </Disclosure>
      )}
    </div>
  );
}
