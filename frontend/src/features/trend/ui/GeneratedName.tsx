import { useTranslation } from "react-i18next";
import { AiNote } from "@/shared/ui/AiNote";
import type { LiveTopic } from "../liveApi";

export function GeneratedName({ topic }: { topic: Pick<LiveTopic, "name" | "label"> }) {
  const { t } = useTranslation("trend");
  if (!topic.label || topic.label === topic.name) return null;
  return <AiNote>{t("signalList.generatedName", { original: topic.name })}</AiNote>;
}
