import { searchTechnologies, type LiveTopic } from "@/features/trend";

const LIMIT = 5;

function words(text: string): string {
  return (
    text
      .toLocaleLowerCase()
      .match(/[\p{L}\d]+/gu)
      ?.join(" ") ?? ""
  );
}

// Направление должно быть названо целиком: совпадение общего слова не доказывает связь с вопросом.
export function matchTopics<T extends Pick<LiveTopic, "source" | "name">>(
  topics: T[],
  question: string,
  name: (topic: T) => string,
) {
  const asked = ` ${words(question)} `;
  return topics
    .filter(
      (topic) =>
        topic.source !== "search" &&
        [name(topic), topic.name].some((label) => {
          const phrase = words(label);
          return phrase !== "" && asked.includes(` ${phrase} `);
        }),
    )
    .slice(0, LIMIT);
}

// Порог релевантности каталога относится ко всему вопросу; поиск отдельных слов его обходил.
export async function catalogueHits(question: string, signal: AbortSignal) {
  const page = await searchTechnologies({ q: question, limit: LIMIT }, signal);
  return page.reason ? [] : page.technologies;
}
