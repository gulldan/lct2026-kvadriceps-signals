import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { ArrowUpRight } from "lucide-react";
import { SearchBar } from "@/features/search/ui/SearchBar";
import { Button } from "@/shared/ui/Button";
import { PageLayout } from "@/shared/ui/PageLayout";
import { PageHeader } from "@/shared/ui/PageHeader";
import { Section } from "@/shared/ui/Section";
import { listTopClusters } from "@/features/cluster";
import { cleanInternalTitle } from "@/features/hypothesis";

interface FocusTopic {
  id: string;
  label: string;
}

// Пул больше показанного, чтобы подсказки менялись от захода к заходу; часть отсеется.
const FOCUS_SHOWN = 6;
const FOCUS_FETCHED = 48;

// По кругу, чтобы подряд не выпадали одни темы; случайное начало — чтобы не с той же шестёрки.
let focusOffset = Math.floor(Math.random() * FOCUS_FETCHED);

function useFocusTopics(): FocusTopic[] {
  const [topics, setTopics] = useState<FocusTopic[]>([]);
  useEffect(() => {
    let cancelled = false;
    listTopClusters(FOCUS_FETCHED)
      .then((clusters) => {
        if (cancelled) return;
        const next: FocusTopic[] = [];
        const seen = new Set<string>();
        for (const cluster of clusters.toSorted((a, b) => b.document_count - a.document_count)) {
          // Старые подписи из ключевых слов склеены через « / »: им место в полном списке тем.
          if (
            cluster.document_count < 2 ||
            cluster.status === "archived" ||
            cluster.label.includes(" / ")
          )
            continue;
          const label = cleanInternalTitle(cluster.label);
          const key = label.toLowerCase();
          if (!label || seen.has(key)) continue;
          seen.add(key);
          next.push({ id: cluster.id, label });
        }
        const start = focusOffset % Math.max(1, next.length);
        focusOffset = start + FOCUS_SHOWN;
        setTopics(
          next.filter((_, index) => (index - start + next.length) % next.length < FOCUS_SHOWN),
        );
      })
      .catch(() => {
        if (!cancelled) setTopics([]);
      });
    return () => {
      cancelled = true;
    };
  }, []);
  return topics;
}

export function SearchHome() {
  const { t } = useTranslation("search");
  const navigate = useNavigate();
  const topics = useFocusTopics();
  return (
    <PageLayout>
      <PageHeader title={t("results.title")} />
      <SearchBar autoFocus onSearch={(q) => navigate(`/search?q=${encodeURIComponent(q)}`)} />

      {topics.length > 0 && (
        <Section
          id="technology-focus"
          title={t("home.focusTitle")}
          className="mt-10 border-t pt-6"
          action={<span className="text-xs text-muted-foreground">{t("home.focusSource")}</span>}
        >
          <div className="-mx-2 mt-3 grid gap-x-6 gap-y-1 sm:grid-cols-2">
            {topics.map((topic) => (
              <Button
                key={topic.id}
                asChild
                variant="ghost"
                size="row"
                className="items-start gap-3"
              >
                <Link to={`/clusters/${topic.id}`}>
                  <span className="min-w-0 flex-1 leading-relaxed text-pretty">{topic.label}</span>
                  <ArrowUpRight className="mt-1 size-3.5 text-muted-foreground" aria-hidden />
                </Link>
              </Button>
            ))}
          </div>
        </Section>
      )}
    </PageLayout>
  );
}
