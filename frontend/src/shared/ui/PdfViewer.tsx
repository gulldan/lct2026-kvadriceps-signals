import { lazy, Suspense, type ComponentProps } from "react";
import { useTranslation } from "react-i18next";
import { Spinner } from "./Spinner";

const PdfViewerInner = lazy(() => import("./PdfViewerInner"));
export type { PdfHighlight } from "./PdfViewerInner";

export function PdfViewer(props: ComponentProps<typeof PdfViewerInner>) {
  const { t } = useTranslation("document");
  return (
    <Suspense
      fallback={
        <div className="flex h-full items-center justify-center gap-2 p-8 text-muted-foreground">
          <Spinner />
          {t("viewer.loadingViewer")}
        </div>
      }
    >
      <PdfViewerInner key={props.url} {...props} />
    </Suspense>
  );
}
