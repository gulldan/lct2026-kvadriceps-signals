import { useCallback, useState } from "react";
import { getDocumentChunks } from "./api";
import { docWithChunks, fileTypeOf, titleOf } from "./docAdapter";
import type { PdfHighlight } from "@/shared/ui/PdfViewer";
import { i18n } from "@/shared/i18n";
import type { KbDoc } from "@/shared/types";

export interface DocPreviewState {
  doc: KbDoc;
  fragmentId?: string;
  highlight?: PdfHighlight;
  full: KbDoc | null;
  failed: boolean;
}

export interface OpenCitationOpts {
  documentId: string;
  snapshotId?: string;
  chunkId?: string;
  filename?: string;
  snippet?: string;
  page?: number;
}

export function useDocPreview() {
  const [preview, setPreview] = useState<DocPreviewState | null>(null);

  const loadChunks = useCallback(async (docId: string, snapshot?: string) => {
    try {
      const { document, chunks } = await getDocumentChunks(docId, snapshot);
      setPreview((p) =>
        p && p.doc.id === docId && p.doc.sourceSnapshot === snapshot
          ? { ...p, full: docWithChunks(document, chunks, snapshot), failed: false }
          : p,
      );
    } catch {
      setPreview((p) =>
        p && p.doc.id === docId && p.doc.sourceSnapshot === snapshot ? { ...p, failed: true } : p,
      );
    }
  }, []);

  const openDoc = useCallback(
    (doc: KbDoc, fragmentId?: string, highlight?: PdfHighlight) => {
      setPreview({ doc, fragmentId, highlight, full: null, failed: false });
      void loadChunks(doc.id, doc.sourceSnapshot);
    },
    [loadChunks],
  );

  const openCitation = useCallback(
    ({ documentId, chunkId, filename, snippet, page, snapshotId }: OpenCitationOpts) => {
      const name = filename ?? i18n.t("document:adapter.document");
      const stub: KbDoc = {
        id: documentId,
        real: true,
        sourceSnapshot: snapshotId,
        hasProvenance: Boolean(snapshotId),
        title: titleOf(name),
        snippet: snippet ?? "",
        section: i18n.t("document:adapter.corpus"),
        sourceId: "backend",
        sourceName: i18n.t("document:adapter.corpus"),
        updatedAt: "",
        tags: [],
        fileType: fileTypeOf(name),
        fileName: name,
        fragments: chunkId
          ? [{ id: chunkId, text: snippet ?? "", location: i18n.t("document:adapter.quote") }]
          : [],
      };
      const trimmed = (snippet ?? "").trim();
      openDoc(stub, chunkId, trimmed === "" ? undefined : { snippet: trimmed, page });
    },
    [openDoc],
  );

  const close = useCallback(() => setPreview(null), []);

  const retry = useCallback(() => {
    setPreview((p) => {
      if (p) void loadChunks(p.doc.id, p.doc.sourceSnapshot);
      return p ? { ...p, full: null, failed: false } : p;
    });
  }, [loadChunks]);

  return { preview, openDoc, openCitation, close, retry };
}
