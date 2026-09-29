import { ArrowLeft, LockKeyhole, Pause, RefreshCw } from "lucide-react";
import { useId, type PointerEvent } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/Button";
import { PageTitle } from "@/shared/ui/PageHeader";

import "./status-page.css";

const STATES = {
  403: "forbidden",
  404: "notFound",
  500: "serverError",
  503: "unavailable",
} as const;

export type PageStatus = keyof typeof STATES;

function followSignal(event: PointerEvent<SVGSVGElement>) {
  if (event.pointerType !== "mouse" || matchMedia("(prefers-reduced-motion: reduce)").matches)
    return;
  const bounds = event.currentTarget.getBoundingClientRect();
  const x = (event.clientX - bounds.left) * (720 / bounds.width) - 360;
  const y = (event.clientY - bounds.top) * (340 / bounds.height) - 170;
  event.currentTarget.style.setProperty("--status-bearing", `${Math.atan2(y, x)}rad`);
}

/** Код остаётся читаемым без движения; рисунок не показывает ход восстановления сервиса. */
function StatusArtwork({ status }: { status: PageStatus }) {
  const id = useId();
  const broken = status === 500;
  return (
    <svg
      key={status}
      data-slot="status-artwork"
      data-status={status}
      viewBox="0 0 720 340"
      className="status-artwork w-full max-w-2xl text-brand select-none"
      aria-hidden="true"
      focusable="false"
      onPointerMove={status === 404 ? followSignal : undefined}
    >
      <defs>
        <clipPath id={`${id}-top`}>
          <path d="M470 40H710V143H470Z" />
        </clipPath>
        <clipPath id={`${id}-middle`}>
          <path d="M470 143H710V202H470Z" />
        </clipPath>
        <clipPath id={`${id}-bottom`}>
          <path d="M470 202H710V310H470Z" />
        </clipPath>
      </defs>
      <g className="status-orbit" fill="none" stroke="currentColor" strokeWidth="1.5">
        <path d="M234 228A138 138 0 0 1 423 47" />
        <path d="M486 112A138 138 0 0 1 297 293" />
        <path d="m416 38 8 9-12 2M304 302l-8-9 12-2" />
      </g>
      <text
        x="105"
        y="264"
        textAnchor="middle"
        className="status-digit status-digit-first fill-current font-mono"
      >
        {broken || status === 503 ? "5" : "4"}
      </text>
      <g className="status-dial" fill="none" stroke="currentColor">
        <circle cx="360" cy="170" r="97" strokeWidth="25" />
        {status === 404 && (
          <>
            <g className="stroke-brand-border" strokeWidth="1.5">
              <circle cx="360" cy="170" r="30" />
              <circle cx="360" cy="170" r="59" />
              <path d="M278 170H442M360 88V252" />
            </g>
            <g transform="translate(360 170)">
              <g className="status-needle">
                <path d="M0 0H76" strokeWidth="2.5" />
                <circle cx="76" cy="0" r="5" className="fill-brand" stroke="none" />
              </g>
            </g>
            <circle cx="360" cy="170" r="6" className="fill-brand" stroke="none" />
          </>
        )}
        {status === 403 && <LockKeyhole x="329" y="136" width="62" height="62" strokeWidth="1.6" />}
        {status === 503 && <Pause x="329" y="139" width="62" height="62" strokeWidth="1.6" />}
        {broken && (
          <g strokeWidth="2.5" strokeLinecap="round">
            <path d="m337 153 14 14m0-14-14 14m32-14 14 14m0-14-14 14M343 195h34" />
          </g>
        )}
      </g>
      {broken ? (
        <g fill="none" stroke="currentColor" strokeWidth="25">
          <g clipPath={`url(#${id}-top)`} className="status-fragment status-fragment-top">
            <ellipse cx="589" cy="170" rx="78" ry="97" />
          </g>
          <g clipPath={`url(#${id}-middle)`} className="status-fragment status-fragment-middle">
            <ellipse cx="589" cy="170" rx="78" ry="97" />
          </g>
          <g clipPath={`url(#${id}-bottom)`} className="status-fragment status-fragment-bottom">
            <ellipse cx="589" cy="170" rx="78" ry="97" />
          </g>
        </g>
      ) : (
        <text
          x="604"
          y="264"
          textAnchor="middle"
          className="status-digit status-digit-last fill-current font-mono"
        >
          {status === 404 ? "4" : "3"}
        </text>
      )}
      {status === 404 && (
        <g className="status-lost-signal">
          <circle cx="477" cy="43" r="12" className="fill-foreground" />
          <path
            d="m468 19 2 6m20 1-5 5m15 14h-7"
            className="stroke-foreground"
            strokeWidth="1.5"
            fill="none"
          />
        </g>
      )}
    </svg>
  );
}

export function StatusPage({
  status,
  onRetry,
  onBack,
  titleAs = "h1",
}: {
  status: PageStatus;
  onRetry?: () => void;
  onBack?: () => void;
  titleAs?: "h1" | "h2";
}) {
  const { t } = useTranslation();
  const titleId = useId();
  const state = STATES[status];
  return (
    <section
      aria-labelledby={titleId}
      className="mx-auto flex w-full max-w-4xl flex-col items-center py-4 text-center sm:py-6"
    >
      <StatusArtwork status={status} />
      <span className="sr-only">{t(`errorPages.${state}.code`)}</span>
      <div className="mt-3 flex min-w-0 flex-col items-center sm:mt-5">
        <PageTitle as={titleAs} id={titleId}>
          {t(`errorPages.${state}.title`)}
        </PageTitle>
        <p className="mt-4 max-w-xl text-base leading-relaxed text-pretty text-muted-foreground">
          {t(`errorPages.${state}.description`)}
        </p>
        <div className="mt-6 flex flex-wrap items-center justify-center gap-3">
          {onRetry && (
            <Button onClick={onRetry}>
              <RefreshCw aria-hidden />
              {t("actions.retry")}
            </Button>
          )}
          {onBack ? (
            <Button variant={onRetry ? "outline" : "default"} onClick={onBack}>
              <ArrowLeft aria-hidden />
              {t("errorPages.toOverview")}
            </Button>
          ) : (
            <Button asChild variant={onRetry ? "outline" : "default"}>
              <a href="/">{t("errorPages.toHome")}</a>
            </Button>
          )}
        </div>
        <p className="mt-7 max-w-lg text-sm leading-relaxed text-pretty text-muted-foreground">
          {t(`errorPages.${state}.hint`)}
        </p>
      </div>
    </section>
  );
}
