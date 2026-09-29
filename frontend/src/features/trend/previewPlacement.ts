type Side = "top" | "right" | "bottom" | "left";
type Point = { x: number; y: number; radius: number };
const clamp = (n: number, max: number) => Math.max(16, Math.min(n, max - 16));

export function previewSide(
  anchor: Point,
  points: Point[],
  viewport: { width: number; height: number },
): Side {
  const gap = anchor.radius + 12;
  const width = Math.min(320, viewport.width - 32);
  const height = 460;
  const candidates: { side: Side; x: number; y: number }[] = [
    { side: "right", x: anchor.x + gap, y: clamp(anchor.y - height / 2, viewport.height - height) },
    {
      side: "left",
      x: anchor.x - gap - width,
      y: clamp(anchor.y - height / 2, viewport.height - height),
    },
    { side: "bottom", x: clamp(anchor.x - width / 2, viewport.width - width), y: anchor.y + gap },
    {
      side: "top",
      x: clamp(anchor.x - width / 2, viewport.width - width),
      y: anchor.y - gap - height,
    },
  ];
  const available = candidates.filter(
    (p) =>
      p.x >= 16 &&
      p.x + width <= viewport.width - 16 &&
      p.y >= 16 &&
      p.y + height <= viewport.height - 16,
  );
  if (!available.length) return anchor.y > viewport.height / 2 ? "top" : "bottom";
  const covered = (p: { x: number; y: number }) =>
    points.filter(
      (point) =>
        point.x + point.radius > p.x &&
        point.x - point.radius < p.x + width &&
        point.y + point.radius > p.y &&
        point.y - point.radius < p.y + height,
    ).length;
  return available.toSorted((a, b) => covered(a) - covered(b))[0]!.side;
}
