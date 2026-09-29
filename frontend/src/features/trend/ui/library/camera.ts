/**
 * Смещения в плоскости камеры: границы учитывают текущий масштаб, форму зала и экрана.
 * При приближении камера ведёт архивариуса, пока человек сам не сдвинет вид; масштаб и
 * смещение догоняют цель плавно, как в игре, а без анимации встают сразу.
 */
export function libraryCamera() {
  let zoom = 1;
  let shown = 1;
  let x = 0;
  let y = 0;
  let manual = false;
  return {
    setZoom(value: number, instant: boolean) {
      zoom = Math.max(1, Math.min(4, value));
      manual = false;
      if (instant) {
        shown = zoom;
        if (zoom === 1) x = y = 0;
      }
    },
    pan(dx: number, dy: number, width: number, height: number, viewHeight: number) {
      if (zoom === 1 || !width || !height) return;
      manual = true;
      const units = viewHeight / shown / height;
      x -= dx * units;
      y += dy * units;
    },
    /** Цель слежения задаётся в тех же единицах, что смещение; `blend` = 1 ставит вид сразу. */
    follow(targetX: number, targetY: number, blend: number) {
      if (manual) return;
      const [goalX, goalY] = zoom === 1 ? [0, 0] : [targetX, targetY];
      x += (goalX - x) * blend;
      y += (goalY - y) * blend;
    },
    advance(dt: number) {
      shown += (zoom - shown) * (1 - Math.exp(-dt * 2.6));
      if (Math.abs(zoom - shown) < 0.001) shown = zoom;
    },
    get shown() {
      return shown;
    },
    frame(viewHeight: number, aspect: number, worldWidth: number, worldHeight: number) {
      const height = viewHeight / shown;
      const limitX = Math.max(0, (worldWidth - height * aspect) / 2 + height * aspect * 0.16);
      const limitY = Math.max(0, (worldHeight - height) / 2 + height * 0.16);
      x = Math.max(-limitX, Math.min(limitX, x));
      y = Math.max(-limitY, Math.min(limitY, y));
      return { height, x, y };
    },
  };
}

/** Перетаскивание включено только при приближении; общий вид оставляет жест прокрутке страницы. */
export function libraryDrag(
  canvas: HTMLCanvasElement,
  enabled: () => boolean,
  pan: (dx: number, dy: number) => void,
) {
  let pointer: { id: number; x: number; y: number } | undefined;
  const down = (event: PointerEvent) => {
    if (!enabled() || event.button !== 0 || pointer) return;
    pointer = { id: event.pointerId, x: event.clientX, y: event.clientY };
    canvas.setPointerCapture(event.pointerId);
  };
  const move = (event: PointerEvent) => {
    if (!pointer || pointer.id !== event.pointerId || !enabled()) return;
    pan(event.clientX - pointer.x, event.clientY - pointer.y);
    pointer = { id: event.pointerId, x: event.clientX, y: event.clientY };
  };
  const release = () => {
    pointer = undefined;
  };
  canvas.addEventListener("pointerdown", down);
  canvas.addEventListener("pointermove", move);
  canvas.addEventListener("pointerup", release);
  canvas.addEventListener("pointercancel", release);
  canvas.addEventListener("lostpointercapture", release);
  return () => {
    canvas.removeEventListener("pointerdown", down);
    canvas.removeEventListener("pointermove", move);
    canvas.removeEventListener("pointerup", release);
    canvas.removeEventListener("pointercancel", release);
    canvas.removeEventListener("lostpointercapture", release);
  };
}
