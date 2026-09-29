import { type ComponentType, Suspense, useEffect, useRef, useState } from "react";
import { Link, NavLink, Outlet, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  Search,
  History,
  FileText,
  LayoutDashboard,
  Users,
  SlidersHorizontal,
  Gauge,
  FlaskConical,
  Layers,
  Target,
  Radar,
  PanelLeftClose,
  PanelLeftOpen,
} from "lucide-react";
import { cn } from "@/shared/lib/cn";
import { Button } from "@/shared/ui/Button";
import { Sheet, SheetContent, SheetTitle } from "@/shared/ui/Sheet";
import { Spinner } from "@/shared/ui/Spinner";
import { Kicker } from "@/shared/ui/Kicker";
import { LanguageSwitcher } from "@/shared/ui/LanguageSwitcher";
import { ThemeSwitcher } from "@/shared/ui/ThemeSwitcher";
import { TaskNotifications } from "@/features/task/ui/TaskNotifications";
import { RoleSwitcher } from "@/features/auth/ui/RoleSwitcher";
import { ErrorBoundary } from "@/app/ErrorPages";
import { useRole } from "@/features/auth/RoleContext";
import { Role } from "@/shared/types";

interface NavItem {
  to: string;
  labelKey:
    | "nav.search"
    | "nav.trends"
    | "nav.hypotheses"
    | "nav.topics"
    | "nav.goals"
    | "nav.history"
    | "nav.documents"
    | "nav.overview"
    | "nav.metrics"
    | "nav.users"
    | "nav.settings";
  icon: ComponentType<{
    className?: string;
  }>;
  end?: boolean;
}

interface NavGroup {
  labelKey: "groups.work" | "groups.corpus" | "groups.admin";
  items: NavItem[];
}

const WORK: NavGroup = {
  labelKey: "groups.work",
  items: [
    { to: "/trends", labelKey: "nav.trends", icon: Radar },
    { to: "/kpi", labelKey: "nav.goals", icon: Target },
    { to: "/hypotheses", labelKey: "nav.hypotheses", icon: FlaskConical },
    { to: "/history", labelKey: "nav.history", icon: History },
  ],
};

const CORPUS: NavGroup = {
  labelKey: "groups.corpus",
  items: [
    { to: "/search", labelKey: "nav.search", icon: Search, end: true },
    { to: "/summary", labelKey: "nav.topics", icon: Layers },
    { to: "/operator/documents", labelKey: "nav.documents", icon: FileText },
  ],
};

const ADMIN: NavGroup = {
  labelKey: "groups.admin",
  items: [
    { to: "/admin", labelKey: "nav.overview", icon: LayoutDashboard, end: true },
    { to: "/admin/metrics", labelKey: "nav.metrics", icon: Gauge },
    { to: "/admin/users", labelKey: "nav.users", icon: Users },
    { to: "/admin/settings", labelKey: "nav.settings", icon: SlidersHorizontal },
  ],
};

const NAV: Record<Role, NavGroup[]> = {
  user: [WORK, CORPUS],
  operator: [WORK, CORPUS],
  admin: [WORK, CORPUS, ADMIN],
};

function AppBrand({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useTranslation("app");
  return (
    <Button asChild variant="ghost" size="content" className="min-w-0 gap-2 text-left">
      <Link to="/" aria-label={t("brand")} onClick={onNavigate}>
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground">
          <Radar className="size-4" aria-hidden />
        </span>
        <span className="font-display block truncate text-sm leading-tight">{t("brand")}</span>
      </Link>
    </Button>
  );
}

function Navigation({
  onNavigate,
  onWarm,
}: {
  onNavigate?: () => void;
  onWarm: (path: string) => void;
}) {
  const { role } = useRole();
  const { t } = useTranslation("app");
  const location = useLocation();
  return (
    <nav className="min-h-0 flex-1 space-y-5 overflow-y-auto p-3" aria-label={t("groups.work")}>
      {NAV[role].map((group) => (
        <div key={group.labelKey}>
          <Kicker className="px-3 pb-1.5">{t(group.labelKey)}</Kicker>
          <div className="space-y-0.5">
            {group.items.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                onClick={onNavigate}
                onMouseEnter={() => onWarm(item.to)}
                onFocus={() => onWarm(item.to)}
                className={({ isActive }) =>
                  cn(
                    "flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors",
                    isActive || (item.to === "/" && location.pathname === "/search")
                      ? "bg-sidebar-primary text-sidebar-primary-foreground"
                      : "text-muted-foreground hover:bg-sidebar-accent hover:text-foreground",
                  )
                }
              >
                <item.icon className="size-4 shrink-0" aria-hidden />
                {t(item.labelKey)}
              </NavLink>
            ))}
          </div>
        </div>
      ))}
    </nav>
  );
}

const SIDEBAR_KEY = "rag.sidebar-collapsed";

export function AppLayout({ onWarm }: { onWarm: (path: string) => void }) {
  const { t, i18n } = useTranslation("app");
  const location = useLocation();
  const mainRef = useRef<HTMLElement>(null);
  const firstRoute = useRef(true);
  // Смена маршрута объявляется заголовком документа и фокусом на содержимом — для читалок экрана.
  useEffect(() => {
    const item = [CORPUS, WORK, ADMIN]
      .flatMap((group) => group.items)
      .find((entry) =>
        entry.end
          ? location.pathname === entry.to
          : location.pathname === entry.to || location.pathname.startsWith(entry.to + "/"),
      );
    document.title = item ? `${t(item.labelKey)} · ${t("brand")}` : t("brand");
    if (firstRoute.current) {
      firstRoute.current = false;
      return;
    }
    if (location.hash) return;
    mainRef.current?.focus({ preventScroll: true });
  }, [location.pathname, location.hash, t]);
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(SIDEBAR_KEY) === "true";
    } catch {
      return false;
    }
  });
  const [mobileOpen, setMobileOpen] = useState(false);
  useEffect(() => {
    const desktop = window.matchMedia("(min-width: 768px)");
    const closeOnDesktop = () => {
      if (desktop.matches) setMobileOpen(false);
    };
    desktop.addEventListener("change", closeOnDesktop);
    return () => desktop.removeEventListener("change", closeOnDesktop);
  }, []);
  const toggleSidebar = () => {
    setCollapsed(!collapsed);
    try {
      localStorage.setItem(SIDEBAR_KEY, String(!collapsed));
    } catch {
      // Без хранилища браузера текущий вид остаётся рабочим.
    }
  };
  return (
    // Смена языка перемонтирует дерево: переводятся и строки из хелперов без useTranslation.
    <div key={i18n.language} className="flex min-h-screen w-full bg-background">
      {/* Иначе клавиатура на каждой загрузке проходит всё меню до содержимого. */}
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-(--layer-toast) focus:rounded-md focus:bg-card focus:px-3 focus:py-2 focus:ring-2 focus:ring-ring focus:outline-none print:hidden"
      >
        {t("skipToContent")}
      </a>
      <aside
        id="desktop-navigation"
        aria-hidden={collapsed}
        inert={collapsed}
        className={cn(
          "duration-normal ease-standard sticky top-0 z-(--layer-sticky) hidden h-dvh shrink-0 overflow-hidden bg-sidebar motion-safe:transition-[width] md:flex print:hidden",
          collapsed ? "w-0" : "w-64 border-r",
        )}
      >
        <div className="flex h-full w-64 shrink-0 flex-col pt-2">
          <Navigation onWarm={onWarm} />
        </div>
      </aside>

      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent
          id="mobile-navigation"
          side="left"
          className="flex flex-col gap-0 p-0"
          aria-describedby={undefined}
        >
          <SheetTitle className="sr-only">{t("menu.title")}</SheetTitle>
          <div className="shrink-0 pt-5 pr-12 pb-4 pl-4">
            <AppBrand onNavigate={() => setMobileOpen(false)} />
          </div>
          <Navigation onNavigate={() => setMobileOpen(false)} onWarm={onWarm} />
        </SheetContent>
      </Sheet>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex flex-wrap items-center justify-between gap-2 border-b bg-background px-4 py-2 print:hidden">
          <div className="flex min-w-0 flex-1 basis-40 items-center gap-2">
            <Button
              variant="ghost"
              size="icon"
              className="hidden shrink-0 md:inline-flex"
              aria-label={t(collapsed ? "menu.expand" : "menu.collapse")}
              title={t(collapsed ? "menu.expand" : "menu.collapse")}
              aria-expanded={!collapsed}
              aria-controls="desktop-navigation"
              onClick={toggleSidebar}
            >
              {collapsed ? <PanelLeftOpen aria-hidden /> : <PanelLeftClose aria-hidden />}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="shrink-0 md:hidden"
              aria-label={t("menu.expand")}
              aria-expanded={mobileOpen}
              aria-controls={mobileOpen ? "mobile-navigation" : undefined}
              onClick={() => setMobileOpen(true)}
            >
              <PanelLeftOpen aria-hidden />
            </Button>
            <AppBrand />
          </div>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <ThemeSwitcher />
            <LanguageSwitcher />
            <TaskNotifications />
            <RoleSwitcher />
          </div>
        </header>
        <main id="main" ref={mainRef} tabIndex={-1} className="min-w-0 flex-1 outline-none">
          <ErrorBoundary key={location.pathname} address={location.pathname + location.search}>
            <Suspense
              fallback={
                <div className="flex min-h-[40vh] items-center justify-center">
                  <Spinner size="lg" />
                </div>
              }
            >
              <Outlet />
            </Suspense>
          </ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
