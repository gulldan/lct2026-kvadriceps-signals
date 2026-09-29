import { Card } from "@/shared/ui/Card";
import { Notice } from "@/shared/ui/Notice";
import {
  createContext,
  memo,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import type { RichTextComponents } from "@/shared/ui/RichText";
import { Copy, Check, ChevronDown, BookOpen } from "lucide-react";
import { toast } from "@/shared/ui/Sonner";
import { PageTitle } from "@/shared/ui/PageHeader";
import { Button } from "@/shared/ui/Button";
import { Skeleton } from "@/shared/ui/Skeleton";
import { Kicker } from "@/shared/ui/Kicker";
import { RichText } from "@/shared/ui/RichText";
import { SearchTrace } from "@/features/search/ui/SearchTrace";
import { AnswerSources } from "@/features/search/ui/AnswerSources";
import type { AnswerMeta } from "@/features/search/api";
import { SmartAnswer as SmartAnswerType, AnswerChip } from "@/shared/types";
import { copyText } from "@/shared/lib/clipboard";

type OpenDoc = (docId: string, fragmentId?: string) => void;

interface AnswerBlockProps {
  /** Тёрн передаётся идентификатором, чтобы колбэк открытия оставался стабильным. */
  turnKey: string;
  question: string;
  answer: SmartAnswerType | null;
  meta?: AnswerMeta;
  loading: boolean;
  selectedDocId?: string;
  onOpenDoc: (turnKey: string, docId: string, fragmentId?: string) => void;
}

const ABSTAIN_KEYS = {
  no_sources: "answer.abstain.noSources",
  weak_evidence: "answer.abstain.weakEvidence",
} as const;

/** Данные сносок передаются через контекст, чтобы компонент ссылки был стабильным. */
const CITE_PREFIX = "#cite-";

const CiteContext = createContext<{
  chips: AnswerChip[];
  onOpen: OpenDoc;
} | null>(null);

function MarkdownLink({ href, children }: { href?: string; children?: ReactNode }) {
  const cite = useContext(CiteContext);
  const idx = href?.startsWith(CITE_PREFIX) ? Number(href.slice(CITE_PREFIX.length)) : NaN;
  const chip = Number.isInteger(idx) ? cite?.chips[idx] : undefined;
  if (chip && cite) return <ChipButton chip={chip} onOpen={cite.onOpen} />;
  return (
    <a href={href} target="_blank" rel="noreferrer">
      {children}
    </a>
  );
}

const CITE_COMPONENTS: RichTextComponents = { a: MarkdownLink };

function ChipButton({ chip, onOpen }: { chip: AnswerChip; onOpen: OpenDoc }) {
  const { t } = useTranslation("search");
  return (
    <Button
      variant="accent"
      size="inline"
      type="button"
      onClick={() => onOpen(chip.docId, chip.fragmentId)}
      className="mx-0.5 min-w-6 px-1 align-text-top font-mono"
      aria-label={t("answer.openSourceAt", { label: chip.label })}
    >
      {chip.label}
    </Button>
  );
}

/** Мемоизирован: просмотр источника не должен заново парсить Markdown и KaTeX всего треда. */
export const AnswerBlock = memo(function AnswerBlock({
  turnKey,
  question,
  answer,
  meta,
  loading,
  selectedDocId,
  onOpenDoc,
}: AnswerBlockProps) {
  const { t } = useTranslation("search");
  const { t: tCommon } = useTranslation();
  const [copied, setCopied] = useState(false);
  const [traceOpen, setTraceOpen] = useState(false);
  const abstain = meta?.trace?.abstainReason ?? "";
  const citedCount = new Set(answer?.chips.map((chip) => chip.docId)).size;

  const openDoc = useCallback<OpenDoc>(
    (docId, fragmentId) => onOpenDoc(turnKey, docId, fragmentId),
    [onOpenDoc, turnKey],
  );
  const citeValue = useMemo(
    () => (answer ? { chips: answer.chips, onOpen: openDoc } : null),
    [answer, openDoc],
  );

  const handleCopy = async () => {
    if (!answer) return;
    if (!(await copyText(answer.plain))) {
      toast.error(tCommon("ui.copyFailed"));
      return;
    }
    setCopied(true);
    toast.success(t("answer.copied"));
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <section aria-label={t("answer.questionAriaLabel", { question })}>
      <Kicker className="text-brand">{t("answer.questionKicker")}</Kicker>
      <PageTitle as="h2" variant="object" className="mt-2">
        {question}
      </PageTitle>

      <Card className="mt-5">
        <div className="flex items-center justify-between border-b px-5 py-3">
          <Kicker>{t("answer.fromDocs")}</Kicker>
          {meta?.cached && <Kicker className="text-muted-foreground">{t("answer.cached")}</Kicker>}
        </div>

        <div className="px-5 py-4">
          {loading ? (
            <div className="space-y-3" aria-live="polite">
              <p className="text-sm text-muted-foreground">{t("answer.searching")}</p>
              <div className="space-y-2.5">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-11/12" />
                <Skeleton className="h-4 w-4/5" />
              </div>
            </div>
          ) : answer ? (
            <>
              {abstain !== "" && (
                <Notice className="mb-3">
                  {t(
                    ABSTAIN_KEYS[abstain as keyof typeof ABSTAIN_KEYS] ??
                      ABSTAIN_KEYS.weak_evidence,
                  )}
                </Notice>
              )}
              <CiteContext.Provider value={citeValue}>
                <RichText className="text-base leading-7" components={CITE_COMPONENTS}>
                  {answer.markdown}
                </RichText>
              </CiteContext.Provider>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">{t("answer.failed")}</p>
          )}
        </div>

        {!loading && answer && (
          <div className="flex flex-wrap items-center gap-2 border-t px-5 py-2.5">
            <span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground">
              <BookOpen className="size-3.5" aria-hidden />
              {citedCount > 0
                ? t("answer.basedOn", { count: citedCount })
                : t("answer.noCitations")}
            </span>
            <div className="ml-auto flex items-center gap-1.5">
              {meta && (
                <Button variant="ghost" size="sm" onClick={() => setTraceOpen((v) => !v)}>
                  {t("trace.title")}
                  <ChevronDown
                    className={`size-3.5 transition-transform ${traceOpen ? "rotate-180" : ""}`}
                    aria-hidden
                  />
                </Button>
              )}
              <Button variant="ghost" size="sm" onClick={handleCopy}>
                {copied ? (
                  <Check className="size-3.5" aria-hidden />
                ) : (
                  <Copy className="size-3.5" aria-hidden />
                )}
                {t("answer.copy")}
              </Button>
            </div>
          </div>
        )}
      </Card>

      {traceOpen && meta && <SearchTrace meta={meta} />}

      {!loading && answer && answer.sources.length > 0 && (
        <AnswerSources answer={answer} selectedDocId={selectedDocId} onOpen={openDoc} />
      )}
    </section>
  );
});
