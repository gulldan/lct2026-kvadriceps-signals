import { useEffect, useRef, useState } from "react";
import { Download, ExternalLink, FileWarning } from "lucide-react";
import { useTranslation } from "react-i18next";
import { fetchDocumentContent } from "@/features/document/api";
import { PdfViewer, type PdfHighlight } from "@/shared/ui/PdfViewer";
import type { FileType } from "@/shared/types";
import { Spinner } from "@/shared/ui/Spinner";

export function OriginalDocViewer({
  docId,
  snapshotID,
  fileName,
  fileType,
  highlight,
  onUnavailable,
}: {
  docId: string;
  snapshotID?: string;
  fileName: string;
  fileType: FileType;
  highlight?: PdfHighlight;
  onUnavailable?: () => void;
}) {
  const { t } = useTranslation("document");
  const [url, setUrl] = useState<string | null>(null);
  const [mime, setMime] = useState("application/octet-stream");
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState(false);
  const onUnavailableRef = useRef(onUnavailable);
  onUnavailableRef.current = onUnavailable;

  useEffect(() => {
    const controller = new AbortController();
    let alive = true;
    let made: string | null = null;
    setUrl(null);
    setText(null);
    setError(false);
    fetchDocumentContent(docId, snapshotID, controller.signal)
      .then(async ({ url: u, mime: m }) => {
        if (!alive) {
          URL.revokeObjectURL(u);
          return;
        }
        made = u;
        setUrl(u);
        setMime(m);
        // Политика безопасности страницы запрещает встраивать текстовые файлы через object.
        if (/^text\/|json|csv/.test(m)) {
          const body = await fetch(u, { signal: controller.signal }).then((r) => r.text());
          if (alive) setText(body);
        }
      })
      .catch(() => {
        if (alive) {
          setError(true);
          onUnavailableRef.current?.();
        }
      });
    return () => {
      alive = false;
      controller.abort();
      if (made) URL.revokeObjectURL(made);
    };
  }, [docId, snapshotID]);

  if (error) {
    return <Centered icon={<FileWarning className="h-6 w-6" />} text={t("viewer.docFailed")} />;
  }
  if (!url) {
    return <Centered icon={<Spinner size="lg" />} text={t("viewer.loadingDoc")} />;
  }

  const isPdf = fileType === "pdf" || mime.includes("pdf");

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-end gap-3 border-b bg-background px-4 py-2 text-sm">
        <a
          href={url}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1.5 text-muted-foreground hover:text-foreground"
        >
          <ExternalLink className="h-4 w-4" /> {t("viewer.openInNewTab")}
        </a>
        <a
          href={url}
          download={fileName}
          className="inline-flex items-center gap-1.5 text-muted-foreground hover:text-foreground"
        >
          <Download className="h-4 w-4" /> {t("actions.download", { ns: "common" })}
        </a>
      </div>
      <div className="min-h-0 flex-1 bg-muted">
        {fileType === "image" ? (
          <div className="flex h-full items-center justify-center overflow-auto p-4">
            <img src={url} alt={fileName} className="max-h-full max-w-full rounded shadow-sm" />
          </div>
        ) : isPdf ? (
          <PdfViewer url={url} highlight={highlight} />
        ) : text !== null ? (
          <div className="h-full overflow-auto bg-background p-6 text-foreground">
            <pre className="mx-auto max-w-prose font-sans text-base leading-7 wrap-anywhere whitespace-pre-wrap">
              {text}
            </pre>
          </div>
        ) : (
          <object data={url} type={mime} className="h-full w-full" aria-label={fileName}>
            <Centered
              icon={<FileWarning className="h-6 w-6" />}
              text={t("viewer.noInlinePreview")}
            />
          </object>
        )}
      </div>
    </div>
  );
}

function Centered({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-2 p-8 text-center text-muted-foreground">
      {icon}
      <p className="text-sm">{text}</p>
    </div>
  );
}
