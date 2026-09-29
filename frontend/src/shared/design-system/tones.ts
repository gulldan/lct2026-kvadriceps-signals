export const badgeTones = {
  default: "border-transparent bg-primary text-primary-foreground",
  secondary: "border-transparent bg-secondary text-secondary-foreground",
  destructive: "border-transparent bg-destructive text-destructive-foreground",
  outline: "text-foreground",
  muted: "border-transparent bg-muted text-muted-foreground",
  brand: "border-transparent bg-brand-wash text-accent-foreground",
  complement: "border-transparent bg-complement-wash text-complement",
  ok: "border-transparent bg-ok-wash text-ok",
  warn: "border-transparent bg-warn-wash text-warn",
  risk: "border-transparent bg-risk-wash text-risk",
  // Поверх пёстрого рисунка текст цвета фона страницы держит контраст в обеих темах.
  "ok-solid": "border-transparent bg-ok text-background",
  "warn-solid": "border-transparent bg-warn text-background",
  "muted-solid": "border-transparent bg-muted-foreground text-background",
  "brand-solid": "border-transparent bg-brand text-brand-foreground",
  "complement-solid": "border-transparent bg-complement text-complement-foreground",
  media: "border-transparent bg-media-ink text-media-ink-foreground",
} as const;

export const chartTones = {
  1: { circle: "border-chart-1 bg-chart-1", field: "bg-chart-1/5", dot: "bg-chart-1" },
  2: { circle: "border-chart-2 bg-chart-2", field: "bg-chart-2/5", dot: "bg-chart-2" },
  3: { circle: "border-chart-3 bg-chart-3", field: "bg-chart-3/5", dot: "bg-chart-3" },
  4: { circle: "border-chart-4 bg-chart-4", field: "bg-chart-4/5", dot: "bg-chart-4" },
  5: { circle: "border-chart-5 bg-chart-5", field: "bg-chart-5/5", dot: "bg-chart-5" },
} as const;

export type Tone = "ok" | "warn" | "risk" | "brand" | "muted";

export const toneClasses = {
  ok: { text: "text-ok", marker: "bg-ok" },
  warn: { text: "text-warn", marker: "bg-warn" },
  risk: { text: "text-risk", marker: "bg-risk" },
  brand: { text: "text-accent-foreground", marker: "bg-brand" },
  muted: { text: "text-muted-foreground", marker: "bg-muted-foreground" },
} as const satisfies Record<Tone, { text: string; marker: string }>;

const STATUS_TONES: Record<string, Tone> = {
  supports: "ok",
  supported: "ok",
  success: "ok",
  indexed: "ok",
  contradicts: "risk",
  refuted: "risk",
  error: "risk",
  failed: "risk",
  mixed: "warn",
  insufficient: "warn",
  running: "brand",
};

export function statusTone(kind: string | undefined, fallback: Tone = "muted"): Tone {
  return STATUS_TONES[kind ?? ""] ?? fallback;
}
