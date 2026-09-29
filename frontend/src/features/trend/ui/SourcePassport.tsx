import { useTranslation } from "react-i18next";
import { Badge } from "@/shared/ui/Badge";

export function SourcePassport({
  publisherType,
  genre,
  sourceName,
  discoverySource,
  trust,
  trustBasis,
  language,
  languageBasis,
}: {
  publisherType?: string;
  genre?: string;
  sourceName?: string;
  discoverySource?: string;
  trust?: string;
  trustBasis?: string;
  language?: string;
  languageBasis?: string;
}) {
  const { t } = useTranslation("trend");
  if (!publisherType && !trust && !language && !genre && !sourceName && !discoverySource)
    return null;
  const tone = trust === "lowered" ? "warn" : trust === "high" ? "ok" : "muted";
  // Видимой строкой, а не в title: title недостижим с клавиатуры и пропадает в печати и в Word.
  const why = [
    sourceName ? t("source.sourceName", { name: sourceName }) : undefined,
    discoverySource
      ? t("source.discoveredVia", {
          source: t(`source.discovery.${discoverySource}`, { defaultValue: discoverySource }),
        })
      : undefined,
    trustBasis,
    languageBasis,
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <p className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
      {publisherType && (
        <Badge variant="muted">
          {t(`source.publisher.${publisherType}`, { defaultValue: publisherType })}
        </Badge>
      )}
      {genre && genre !== "unknown" && (
        <Badge variant="outline">{t(`source.genre.${genre}`, { defaultValue: genre })}</Badge>
      )}
      {language && (
        <span>
          {t("source.language", {
            language: t(`source.languages.${language}`, { defaultValue: language.toUpperCase() }),
          })}
        </span>
      )}
      {trust && <Badge variant={tone}>{t(`source.trust.${trust}`, { defaultValue: trust })}</Badge>}
      {trust === "lowered" && <span>{t("source.trustWarning")}</span>}
      {why && <span className="basis-full">{why}</span>}
    </p>
  );
}
