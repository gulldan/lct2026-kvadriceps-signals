import "./index.css";
import { createRoot } from "react-dom/client";

import { App, warmStartRoute } from "@/app/App";
import { restoreAuth } from "@/shared/api/client";
import { initI18n } from "@/shared/i18n";
import { installMonitor } from "@/shared/lib/monitor";
import { bootTheme } from "@/shared/theme";

installMonitor();
bootTheme();
// Сессия проверяется, пока грузятся словари: иначе запрос уходил только после первой отрисовки.
void restoreAuth();
const dictionaries = initI18n();
warmStartRoute();
await dictionaries;

const container = document.getElementById("root");
if (!container) throw new Error("root element not found");
createRoot(container).render(<App />);
