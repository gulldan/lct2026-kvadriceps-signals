import "./index.css";
import { createRoot } from "react-dom/client";

import { App } from "@/app/App";
import { initI18n } from "@/shared/i18n";
import { installMonitor } from "@/shared/lib/monitor";
import { bootTheme } from "@/shared/theme";

installMonitor();
bootTheme();
await initI18n();

const container = document.getElementById("root");
if (!container) throw new Error("root element not found");
createRoot(container).render(<App />);
