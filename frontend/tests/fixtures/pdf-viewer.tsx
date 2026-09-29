import "../../src/index.css";
import { createRoot } from "react-dom/client";
import { initI18n, loadNamespace } from "@/shared/i18n";
import { PdfViewer } from "@/shared/ui/PdfViewer";

await initI18n();
// Страница — не маршрут приложения, поэтому ленивый словарь документов добирает сама.
await loadNamespace("document");
createRoot(document.getElementById("root")!).render(
  <main className="h-dvh">
    <PdfViewer url="/tests/fixtures/one-page.pdf" />
  </main>,
);
