import { useTranslation } from "react-i18next";
import { Disclosure } from "@/shared/ui/Disclosure";
import { formatNumber } from "@/shared/lib/format";
import { liveLimitFailure, liveLimitKind } from "../liveLimits";
import type { LiveLimit } from "../liveApi";

/** Строка без кода (старые снимки, патенты, веб) обязана прочитаться, а не уронить карточку. */
export function LiveLimits({ limits }: { limits: (LiveLimit | string)[] }) {
  const { t, i18n } = useTranslation("trend");
  if (!limits.length) return null;
  // Текст сборщика русский: в английском и китайском интерфейсе остаётся оговорка словаря.
  const keepRaw = i18n.language.startsWith("ru");
  // По готовой строке, а не по коду: разные коды без чисел сходятся в одну оговорку.
  const failures = new Set<string>();
  const bounds = new Set<string>();
  for (const entry of limits) {
    const limit: LiveLimit = typeof entry === "string" ? { text: entry } : entry;
    const code = limit.code || liveLimitKind(limit.text);
    const line = t(`study.live.coverage.${code}`, {
      // i18next подставляет значение как есть, а числа пишутся «1 241».
      replace: Object.fromEntries(
        Object.entries(limit.values ?? {}).map(([name, value]) => [name, formatNumber(value)]),
      ),
      defaultValue: t("study.live.coverage.other"),
    });
    // Строку с «{{» (код без чисел) печатать нельзя; русская оговорка сборщика точнее общей фразы.
    const neutral = t("study.live.coverage.other");
    const unusable = line.includes("{{") || line === neutral;
    const text = unusable ? (keepRaw && russian(limit.text)) || neutral : line;
    (liveLimitFailure(code) ? failures : bounds).add(text);
  }
  const lines = [...failures, ...bounds];
  return (
    <output className="block">
      <Disclosure
        className="border-t-0 [&>div]:pb-0"
        summaryClassName="py-1 text-sm font-normal text-muted-foreground"
        title={t(
          failures.size ? "study.live.coverage.summaryFailures" : "study.live.coverage.summary",
          {
            total: lines.length,
            failures: failures.size,
          },
        )}
      >
        <div className="space-y-1 text-sm text-muted-foreground">
          {lines.map((line) => (
            <p key={line}>{line}</p>
          ))}
        </div>
      </Disclosure>
    </output>
  );
}

function russian(text: string): string | null {
  const body = text.replace(/^[a-z_]+:\s*/, "").trim();
  if (!/[а-яё]/i.test(body)) return null;
  return body.charAt(0).toUpperCase() + body.slice(1);
}
