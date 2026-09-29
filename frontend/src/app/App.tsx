import { type ComponentType, type ReactNode, useEffect, useRef } from "react";
import {
  BrowserRouter,
  Navigate,
  Route,
  Routes,
  useLocation,
  useNavigationType,
  useParams,
  useSearchParams,
} from "react-router-dom";

import { AppLayout } from "@/app/AppLayout";
// Посадочная страница — не ленивая: ленивый чанк на первом рендере даёт заслон.
import { TrendLivePage } from "@/features/trend/pages/TrendLivePage";
import { TrendWorkspace, TrendCatalogueLoading } from "@/features/trend/ui/TrendWorkspace";
import { ErrorBoundary, Forbidden, NotFound, StandaloneErrorPage } from "@/app/ErrorPages";
import { AuthProvider, useAuth } from "@/features/auth/AuthContext";
import { Login } from "@/features/auth/pages/Login";
import { RoleProvider } from "@/features/auth/RoleContext";
import { TaskProvider } from "@/features/task/TaskContext";
import { type LazyNamespace, loadNamespace } from "@/shared/i18n";
import { loadable } from "@/shared/lib/loadable";
import { prefetch } from "@/shared/lib/useRequest";
import { Toaster } from "@/shared/ui/Sonner";
import { Spinner } from "@/shared/ui/Spinner";
import { TooltipProvider } from "@/shared/ui/Tooltip";
import type { Role } from "@/shared/types";

// Хвостовой список ns обязан называть каждый ленивый неймспейс страницы, включая чужие: они
// грузятся вместе с чанком.
const PREFETCH: (() => Promise<unknown>)[] = [];

const WARM = new Set(["Hypotheses", "SummaryPage", "SearchHome", "OperatorDocuments", "KpiPage"]);

// Почему не React.lazy — в shared/lib/loadable.
function page<K extends string>(
  load: () => Promise<{ [P in K]: ComponentType }>,
  name: K,
  ...ns: LazyNamespace[]
) {
  const Page = loadable(
    () => Promise.all([load(), ...ns.map(loadNamespace)]).then(([module]) => module[name]),
    <div className="flex min-h-[40vh] items-center justify-center">
      <Spinner size="lg" />
    </div>,
  );
  if (WARM.has(name)) PREFETCH.push(Page.warm);
  return Page;
}

function PrefetchPages() {
  const { pathname, search } = useLocation();
  const landing = search === "" && (pathname === "/" || pathname === "/trends");
  useEffect(() => {
    // Снимок радара уходит до монтирования; страница заберёт ответ по тому же ключу.
    if (!landing) return;
    void import("@/features/trend/liveApi").then(({ getLiveOverview }) =>
      prefetch("live-overview", getLiveOverview),
    );
  }, [landing]);
  useEffect(() => {
    // В dev-сервере модули отдаются поштучно, и прогрев только мешает тестам.
    if (!import.meta.env.PROD) return;
    const connection = (
      navigator as { connection?: { saveData?: boolean; effectiveType?: string } }
    ).connection;
    if (connection?.saveData || /2g/.test(connection?.effectiveType ?? "")) return;
    let cancelled = false;
    const warm = async () => {
      for (const load of PREFETCH) {
        if (cancelled) return;
        await load().catch(() => undefined);
      }
    };
    const idle = window.requestIdleCallback ?? ((fn: () => void) => window.setTimeout(fn, 1500));
    const cancel = window.cancelIdleCallback ?? window.clearTimeout;
    const handle = idle(() => void warm(), { timeout: 4000 });
    return () => {
      cancelled = true;
      cancel(handle);
    };
  }, []);
  return null;
}

const SearchHome = page(() => import("@/features/search/pages/SearchHome"), "SearchHome", "search");
const SearchResults = page(
  () => import("@/features/search/pages/SearchResults"),
  "SearchResults",
  "document",
  "hypothesis",
  "search",
);
const UserHistory = page(
  () => import("@/features/search/pages/UserHistory"),
  "UserHistory",
  "search",
);
const Hypotheses = page(
  () => import("@/features/hypothesis/pages/Hypotheses"),
  "Hypotheses",
  // admin — карточка весов скоринга, общая со страницей настроек.
  "admin",
  "document",
  "hypothesis",
  "hypothesisDetail",
  "kpi",
);
const HypothesisPage = page(
  () => import("@/features/hypothesis/pages/HypothesisPage"),
  "HypothesisPage",
  "document",
  "hypothesis",
  "hypothesisDetail",
);
const HypothesisReport = page(
  () => import("@/features/hypothesis/pages/HypothesisReport"),
  "HypothesisReport",
  "hypothesis",
);
const HypothesisReportPage = page(
  () => import("@/features/hypothesis/pages/HypothesisReportPage"),
  "HypothesisReportPage",
  "hypothesis",
);
const ClusterPage = page(
  () => import("@/features/cluster/pages/ClusterPage"),
  "ClusterPage",
  "cluster",
  "document",
  "hypothesisDetail",
);
const SourcePage = page(
  () => import("@/features/document/pages/SourcePage"),
  "SourcePage",
  "document",
);
const TrendRadar = page(() => import("@/features/trend/pages/TrendRadar"), "TrendRadar");
const TrendStudyPage = page(
  () => import("@/features/trend/pages/TrendStudyPage"),
  "TrendStudyPage",
);
const TrendPage = page(
  () => import("@/features/trend/pages/TrendPage"),
  "TrendPage",
  // document — подписи страниц и фрагментов в свидетельствах направления.
  "document",
);
const TrendMethod = page(() => import("@/features/trend/pages/TrendMethod"), "TrendMethod");
const TrendModelCheck = page(
  () => import("@/features/trend/pages/TrendModelCheck"),
  "TrendModelCheck",
);
const TrendExample = page(() => import("@/features/trend/pages/TrendExample"), "TrendExample");
const TechnologyCatalogue = loadable(
  () => import("@/features/trend/pages/TechnologyCatalogue").then((m) => m.TechnologyCatalogue),
  <TrendCatalogueLoading />,
);
const TechnologyCardPage = page(
  () => import("@/features/trend/pages/TechnologyCardPage"),
  "TechnologyCardPage",
);
const KpiPage = page(() => import("@/features/kpi/pages/KpiPage"), "KpiPage", "document", "kpi");
const ThesisPage = page(() => import("@/features/kpi/pages/ThesisPage"), "ThesisPage", "kpi");
const SummaryPage = page(
  () => import("@/features/summary/pages/SummaryPage"),
  "SummaryPage",
  "summary",
  "hypothesisDetail",
);
const OperatorDocuments = page(
  () => import("@/features/document/pages/OperatorDocuments"),
  "OperatorDocuments",
  "document",
);
const AdminDashboard = page(
  () => import("@/features/admin/pages/AdminDashboard"),
  "AdminDashboard",
  "admin",
);
const AdminUsers = page(() => import("@/features/admin/pages/AdminUsers"), "AdminUsers", "admin");
const AdminSettings = page(
  () => import("@/features/admin/pages/AdminSettings"),
  "AdminSettings",
  "admin",
);
const AdminMetrics = page(
  () => import("@/features/admin/pages/AdminMetrics"),
  "AdminMetrics",
  "admin",
);

// Радара здесь нет намеренно: он грузится со стартом приложения.
const MENU_PAGES: Record<string, { warm: () => Promise<unknown> }> = {
  "/kpi": KpiPage,
  "/hypotheses": Hypotheses,
  "/search": SearchHome,
  "/summary": SummaryPage,
  "/history": UserHistory,
  "/operator/documents": OperatorDocuments,
  "/admin": AdminDashboard,
  "/admin/metrics": AdminMetrics,
  "/admin/users": AdminUsers,
  "/admin/settings": AdminSettings,
};

function warmRoute(path: string) {
  void MENU_PAGES[path]?.warm();
}

function RequireAuth({ children }: { children: ReactNode }) {
  const { auth } = useAuth();
  const location = useLocation();
  if (!auth) {
    return (
      <Navigate
        to="/login"
        replace
        state={{ from: location.pathname + location.search + location.hash }}
      />
    );
  }
  return (
    <>
      <PrefetchPages />
      {children}
    </>
  );
}

function RequireRole({ roles, children }: { roles: Role[]; children: ReactNode }) {
  const { roles: have } = useAuth();
  if (!roles.some((r) => have.includes(r))) {
    return <Forbidden />;
  }
  return <>{children}</>;
}

const guard = (roles: Role[], element: ReactNode) => (
  <RequireRole roles={roles}>{element}</RequireRole>
);

/** Сохранённые ответы ссылаются на «/documents/:id», а страница источника — «/sources/:id». */
function DocumentAlias() {
  const { id = "" } = useParams();
  return <Navigate to={`/sources/${encodeURIComponent(id)}`} replace />;
}

function SearchPage() {
  const [params] = useSearchParams();
  return params.get("q")?.trim() || params.get("chat") ? <SearchResults /> : <SearchHome />;
}

function TrendsPage() {
  const [params] = useSearchParams();
  // Литерал повторён намеренно: импорт словаря из features/trend/scope тянет его в стартовый бандл.
  if (params.get("entity") === "technologies")
    return (
      <TrendWorkspace>
        <TechnologyCatalogue />
      </TrendWorkspace>
    );
  if (params.has("run") || params.has("request") || params.get("view") === "analysis")
    return <TrendRadar />;
  return params.get("view") === "study" ? (
    <TrendStudyPage />
  ) : (
    <TrendWorkspace>
      <TrendLivePage />
    </TrendWorkspace>
  );
}

function ScrollToTop() {
  const { pathname } = useLocation();
  const navigation = useNavigationType();
  const previous = useRef(pathname);
  useEffect(() => {
    // Сравнивается путь: переключатель вида меняет только параметр адреса и не поднимает страницу.
    if (previous.current === pathname) return;
    previous.current = pathname;
    if (navigation !== "POP") window.scrollTo({ top: 0 });
  }, [pathname, navigation]);
  return null;
}

export function App() {
  return (
    <AuthProvider>
      <RoleProvider>
        <TooltipProvider>
          <BrowserRouter>
            <ScrollToTop />
            <ErrorBoundary standalone>
              <Routes>
                <Route path="/login" element={<Login />} />
                <Route path="/403" element={<StandaloneErrorPage status={403} />} />
                <Route path="/404" element={<StandaloneErrorPage status={404} />} />
                <Route path="/500" element={<StandaloneErrorPage status={500} />} />
                <Route path="/503" element={<StandaloneErrorPage status={503} />} />
                <Route
                  element={
                    <RequireAuth>
                      <TaskProvider>
                        <AppLayout onWarm={warmRoute} />
                      </TaskProvider>
                    </RequireAuth>
                  }
                >
                  <Route path="/" element={<Navigate to="/trends" replace />} />
                  <Route path="/search" element={<SearchPage />} />
                  <Route path="/hypotheses" element={<Hypotheses />} />
                  <Route path="/hypotheses/report" element={<HypothesisReport />} />
                  <Route path="/hypotheses/:id/report" element={<HypothesisReportPage />} />
                  <Route path="/hypotheses/:id" element={<HypothesisPage />} />
                  <Route path="/directions/:id" element={<HypothesisPage />} />
                  <Route path="/clusters/:id" element={<ClusterPage />} />
                  <Route path="/sources/:id" element={<SourcePage />} />
                  <Route path="/documents/:id" element={<DocumentAlias />} />
                  <Route path="/trends" element={<TrendsPage />} />
                  <Route path="/technologies/:id" element={<TechnologyCardPage />} />
                  <Route path="/trends/example" element={<TrendExample />} />
                  <Route path="/trends/method" element={<TrendMethod />} />
                  <Route path="/trends/model" element={<TrendModelCheck />} />
                  <Route path="/trends/:run/topics/:candidate" element={<TrendPage />} />
                  <Route path="/trends/:run/method" element={<TrendMethod />} />
                  <Route path="/summary" element={<SummaryPage />} />
                  <Route path="/kpi" element={<KpiPage />} />
                  <Route path="/kpi/:id" element={<ThesisPage />} />
                  <Route path="/history" element={<UserHistory />} />
                  <Route
                    path="/operator/documents"
                    element={guard(["user", "operator", "admin"], <OperatorDocuments />)}
                  />
                  <Route path="/admin" element={guard(["admin"], <AdminDashboard />)} />
                  <Route path="/admin/users" element={guard(["admin"], <AdminUsers />)} />
                  <Route path="/admin/settings" element={guard(["admin"], <AdminSettings />)} />
                  <Route
                    path="/admin/metrics"
                    element={guard(["admin", "operator"], <AdminMetrics />)}
                  />
                  <Route path="*" element={<NotFound />} />
                </Route>
              </Routes>
            </ErrorBoundary>
          </BrowserRouter>
          <Toaster />
        </TooltipProvider>
      </RoleProvider>
    </AuthProvider>
  );
}
