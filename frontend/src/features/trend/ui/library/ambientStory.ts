import { Group } from "three";
import type { AmbientKind, AmbientScheduler } from "./ambientScheduler";

const storyEase = (value: number) => {
  const clamped = Math.max(0, Math.min(1, value));
  return clamped * clamped * (3 - 2 * clamped);
};

export const storyStep = (progress: number, from: number, to: number) =>
  storyEase((progress - from) / (to - from));

export const storyEnvelope = (progress: number, open: number, close: number) =>
  storyStep(progress, 0, open) * (1 - storyStep(progress, close, 1));

/** Завершение закрепляет результат один раз; тихий режим только успокаивает позу. */
export function ambientStory(
  scheduler: AmbientScheduler,
  kind: AmbientKind,
  object: Group,
  render: (progress: number, variant: number, quiet: boolean) => void,
  start?: (variant: number) => void,
  complete?: () => void,
) {
  let serial = 0;
  let variant = 0;
  let active = false;
  object.userData.completed = 0;
  object.userData.phase = "rest";
  function advance(still: boolean) {
    if (still) {
      render(1, variant, true);
      object.userData.phase = "rest";
      return;
    }
    const event = scheduler.event(kind);
    if (event) {
      if (event.serial !== serial) {
        serial = event.serial;
        variant = event.variant;
        start?.(variant);
      }
      active = true;
      render(Math.min(1, (scheduler.time() - event.start) / event.duration), variant, false);
    } else if (active) {
      render(1, variant, false);
      complete?.();
      active = false;
      object.userData.completed = Number(object.userData.completed) + 1;
      object.userData.phase = "rest";
    }
  }
  advance(true);
  return { advance };
}
