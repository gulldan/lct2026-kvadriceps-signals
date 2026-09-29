import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowUpRight } from "lucide-react";
import {
  getLiveOverview,
  stageTones,
  technologyPath,
  type LiveOverview,
  type LiveTopic,
  type TechnologyPage,
} from "@/features/trend";
import { useRequest } from "@/shared/lib/useRequest";
import { Badge } from "@/shared/ui/Badge";
import { Button } from "@/shared/ui/Button";
import { Kicker } from "@/shared/ui/Kicker";
import { catalogueHits, matchTopics } from "../searchEntities";

const CACHE_MS = 5 * 60_000;

export function SearchEntities({ question }: { question: string }) {
  const { t } = useTranslation("search");
  const { t: tTrend } = useTranslation("trend");
  // Русские названия направлений — в ленивом словаре радара, а здесь он нужен раньше.
  const [namesReady, setNamesReady] = useState(false);
  useEffect(() => {
    setNamesReady(true);
  }, []);
  const overview = useRequest<LiveOverview>("live-overview", getLiveOverview, {
    keepPrevious: true,
    cacheMs: CACHE_MS,
  });
  const technologies = useRequest<TechnologyPage["technologies"]>(
    `search-technologies:${question}`,
    (signal) => catalogueHits(question, signal),
    { cacheMs: CACHE_MS },
  );
  const name = (topic: LiveTopic) => {
    const raw =
      topic.label || tTrend(`study.live.topics.${topic.id}`, { defaultValue: topic.name });
    return raw.charAt(0).toUpperCase() + raw.slice(1);
  };
  const topics = namesReady ? matchTopics(overview.data?.topics ?? [], question, name) : [];
  const hits = technologies.data ?? [];
  const loading = overview.status === "loading" || technologies.status === "loading";
  const failed = overview.status === "error" && technologies.status === "error";
  const radar = (
    <Button variant="link" size="inline" asChild>
      <Link to={`/trends?q=${encodeURIComponent(question)}`}>
        {t("entities.radar")}
        <ArrowUpRight aria-hidden />
      </Link>
    </Button>
  );
  return (
    <div className="mt-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Kicker>{t("entities.heading")}</Kicker>
        {radar}
      </div>
      {failed ? (
        <p className="mt-2 text-sm text-muted-foreground">{t("entities.error")}</p>
      ) : !loading && hits.length === 0 && topics.length === 0 ? (
        <p className="mt-2 text-sm text-muted-foreground">{t("entities.empty")}</p>
      ) : (
        <div className="mt-2 grid gap-x-8 gap-y-4 sm:grid-cols-2">
          {hits.length > 0 && (
            <section aria-label={t("entities.technologies")} className="min-w-0">
              <h3 className="text-sm font-medium">{t("entities.technologies")}</h3>
              <ul className="mt-1 divide-y">
                {hits.map((hit) => (
                  <li key={hit.technology_id} className="py-2">
                    <Button variant="link" size="inline" asChild className="text-left font-medium">
                      <Link
                        to={technologyPath(
                          hit.technology_id,
                          `/search?q=${encodeURIComponent(question)}`,
                        )}
                      >
                        {hit.canonical_name}
                      </Link>
                    </Button>
                    {hit.description && (
                      <p className="mt-0.5 line-clamp-2 text-sm text-muted-foreground">
                        {hit.description}
                      </p>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {topics.length > 0 && (
            <section aria-label={t("entities.directions")} className="min-w-0">
              <h3 className="text-sm font-medium">{t("entities.directions")}</h3>
              <ul className="mt-1 divide-y">
                {topics.map((topic) => (
                  <li key={topic.id} className="flex flex-wrap items-center gap-2 py-2">
                    <Button variant="link" size="inline" asChild className="text-left font-medium">
                      <Link to={`/trends?topic=${encodeURIComponent(topic.id)}`}>
                        {name(topic)}
                      </Link>
                    </Button>
                    <Badge variant={stageTones[topic.stage].badge}>
                      {tTrend(`study.live.table.stages.${topic.stage}`)}
                    </Badge>
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      )}
    </div>
  );
}
