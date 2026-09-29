import { readColorToken } from "@/shared/design-system/colors";
import type { ViewNode, ViewLink, GraphTransform } from "./graphModel";
const OVERVIEW_LINK_LIMIT = 780;
const LABELS_ZOOM = 1.45;
function truncate(s: string, max: number): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}
interface CanvasPalette {
  brand: string;
  ok: string;
  risk: string;
  warn: string;
  mixed: string;
  muted: string;
  border: string;
  card: string;
  foreground: string;
  fontFamily: string;
}

const paletteCache = new WeakMap<HTMLCanvasElement, { key: string; palette: CanvasPalette }>();

function themeKey(): string {
  const explicit = document.documentElement.dataset.theme;
  if (explicit) return explicit;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function paletteFor(canvas: HTMLCanvasElement): CanvasPalette {
  const key = themeKey();
  const cached = paletteCache.get(canvas);
  if (cached && cached.key === key) return cached.palette;
  const palette = computePalette(canvas);
  paletteCache.set(canvas, { key, palette });
  return palette;
}

function computePalette(canvas: HTMLCanvasElement): CanvasPalette {
  return {
    brand: readColorToken("brand", canvas),
    ok: readColorToken("ok", canvas),
    risk: readColorToken("risk", canvas),
    warn: readColorToken("warn", canvas),
    mixed: readColorToken("mixed", canvas),
    muted: readColorToken("muted-foreground", canvas),
    border: readColorToken("border", canvas),
    card: readColorToken("card", canvas),
    foreground: readColorToken("foreground", canvas),
    fontFamily: getComputedStyle(canvas).fontFamily,
  };
}

function nodeFill(node: ViewNode, palette: CanvasPalette): string {
  if (node.kind === "document") return palette.muted;
  if (node.verdict === "supported") return palette.ok;
  if (node.verdict === "refuted") return palette.risk;
  if (node.verdict === "mixed" || node.verdict === "insufficient") return palette.warn;
  return palette.brand;
}

function linkColor(link: ViewLink, palette: CanvasPalette): string {
  if (link.class === "supports") return palette.ok;
  if (link.class === "contradicts") return palette.risk;
  if (link.class === "mixed") return palette.mixed;
  return palette.border;
}

function roundedRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
) {
  const radius = Math.min(r, w / 2, h / 2);
  ctx.beginPath();
  ctx.moveTo(x + radius, y);
  ctx.arcTo(x + w, y, x + w, y + h, radius);
  ctx.arcTo(x + w, y + h, x, y + h, radius);
  ctx.arcTo(x, y + h, x, y, radius);
  ctx.arcTo(x, y, x + w, y, radius);
  ctx.closePath();
}

function prepareCanvas(canvas: HTMLCanvasElement) {
  const rect = canvas.getBoundingClientRect();
  const dpr = Math.max(1, window.devicePixelRatio || 1);
  const width = Math.max(1, Math.round(rect.width * dpr));
  const height = Math.max(1, Math.round(rect.height * dpr));
  if (canvas.width !== width || canvas.height !== height) {
    canvas.width = width;
    canvas.height = height;
  }
  const ctx = canvas.getContext("2d");
  if (!ctx) return null;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, rect.width, rect.height);
  return { ctx, w: rect.width, h: rect.height };
}

function drawLabel(
  ctx: CanvasRenderingContext2D,
  node: ViewNode,
  text: string,
  palette: CanvasPalette,
  k: number,
  strong: boolean,
) {
  const fontSize = (strong ? 12 : 10.5) / k;
  ctx.font = `${strong ? 600 : 500} ${fontSize}px ${palette.fontFamily}`;
  ctx.textBaseline = "middle";
  const label = truncate(text, strong ? 44 : 32);
  const textW = ctx.measureText(label).width;
  const x = node.x + node.r + 7 / k;
  const y = node.y;
  if (strong) {
    roundedRect(ctx, x - 5 / k, y - 10 / k, textW + 10 / k, 20 / k, 6 / k);
    ctx.fillStyle = palette.card;
    ctx.globalAlpha = 0.9;
    ctx.fill();
    ctx.globalAlpha = 1;
  }
  ctx.fillStyle = palette.foreground;
  ctx.fillText(label, x, y);
}

export function drawGraph({
  canvas,
  nodes,
  links,
  transform,
  activeId,
  selectedId,
  labelIds,
  neighbors,
}: {
  canvas: HTMLCanvasElement;
  nodes: ViewNode[];
  links: ViewLink[];
  transform: GraphTransform;
  activeId: string | null;
  selectedId: string | null;
  labelIds: Set<string>;
  neighbors: Set<string> | null;
}) {
  const prepared = prepareCanvas(canvas);
  if (!prepared) return;
  const { ctx, w, h } = prepared;
  const palette = paletteFor(canvas);
  const nodeById = new Map(nodes.map((node) => [node.id, node]));

  ctx.save();
  ctx.translate(w / 2 + transform.x, h / 2 + transform.y);
  ctx.scale(transform.k, transform.k);
  ctx.lineCap = "round";

  links.forEach((link, index) => {
    const source = nodeById.get(link.source);
    const target = nodeById.get(link.target);
    if (!source || !target) return;
    const focused = activeId !== null && (link.source === activeId || link.target === activeId);
    const near = neighbors !== null && neighbors.has(link.source) && neighbors.has(link.target);
    if (activeId === null && index > OVERVIEW_LINK_LIMIT) return;
    if (activeId !== null && !near && index > 340) return;

    const dx = target.x - source.x;
    const dy = target.y - source.y;
    const len = Math.max(1, Math.hypot(dx, dy));
    const bendSeed = (source.id.charCodeAt(0) + target.id.charCodeAt(target.id.length - 1)) % 17;
    const bend = ((bendSeed - 8) * 2.2) / transform.k;
    const mx = (source.x + target.x) / 2 - (dy / len) * bend;
    const my = (source.y + target.y) / 2 + (dx / len) * bend;

    ctx.beginPath();
    ctx.moveTo(source.x, source.y);
    ctx.quadraticCurveTo(mx, my, target.x, target.y);
    ctx.strokeStyle = linkColor(link, palette);
    ctx.lineWidth =
      (focused
        ? 2.3 + Math.min(2.2, Math.sqrt(link.weight) * 0.5)
        : 0.75 + Math.min(1.4, Math.sqrt(link.weight) * 0.2)) / transform.k;
    ctx.globalAlpha =
      activeId === null
        ? link.class === "context"
          ? 0.12
          : 0.2
        : focused
          ? 0.9
          : near
            ? 0.22
            : 0.045;
    ctx.stroke();
  });

  const ordered = nodes.toSorted((a, b) =>
    a.kind === b.kind ? a.degree - b.degree : a.kind === "document" ? -1 : 1,
  );
  for (const node of ordered) {
    const focused = activeId === node.id;
    const selected = selectedId === node.id;
    const near = neighbors === null || neighbors.has(node.id);
    const radius = Math.max(
      node.r,
      node.kind === "hypothesis" ? 4.8 / transform.k : 3.2 / transform.k,
    );

    ctx.beginPath();
    ctx.arc(node.x, node.y, radius, 0, Math.PI * 2);
    ctx.fillStyle = nodeFill(node, palette);
    ctx.globalAlpha = near ? 1 : 0.16;
    ctx.fill();
    ctx.strokeStyle = selected || focused ? palette.foreground : palette.card;
    ctx.lineWidth =
      (selected || focused ? 2.5 : node.kind === "hypothesis" ? 1.8 : 1.1) / transform.k;
    ctx.globalAlpha = near ? 1 : 0.2;
    ctx.stroke();
  }

  for (const node of nodes) {
    const isFocus = activeId === node.id || selectedId === node.id;
    const shouldLabel =
      isFocus ||
      labelIds.has(node.id) ||
      (node.kind === "hypothesis" && transform.k >= LABELS_ZOOM && node.degree > 0) ||
      (node.kind === "document" && transform.k >= 2.35 && (neighbors?.has(node.id) ?? false));
    if (!shouldLabel) continue;
    const near = neighbors === null || neighbors.has(node.id);
    if (!near && !isFocus) continue;
    ctx.globalAlpha = near || isFocus ? 1 : 0.22;
    drawLabel(ctx, node, node.label, palette, transform.k, isFocus);
  }

  ctx.restore();
  ctx.globalAlpha = 1;
}
