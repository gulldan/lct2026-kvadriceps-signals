import type { StudyTopic } from "./studyApi";

const TONES = {
  emerging_signal: {
    marker: "bg-complement",
    text: "text-complement",
    badge: "complement",
    border: "border-l-complement",
  },
  early_signal: { marker: "bg-warn", text: "text-warn", badge: "warn", border: "border-l-warn" },
  established: {
    marker: "bg-foreground",
    text: "text-foreground",
    badge: "secondary",
    border: "border-l-foreground",
  },
  unassessed: {
    marker: "bg-muted-foreground",
    text: "text-muted-foreground",
    badge: "muted",
    border: "border-l-border",
  },
} as const satisfies Record<
  StudyTopic["stage"],
  { marker: string; text: string; badge: string; border: string }
>;

/** Незнакомая стадия читается как неоценённая, а не роняет радар на undefined. */
export const stageTones = new Proxy(TONES, {
  get: (tones, stage) => tones[stage as keyof typeof TONES] ?? tones.unassessed,
}) as typeof TONES;
