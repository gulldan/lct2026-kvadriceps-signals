import { useCallback, useEffect, useRef, useState } from "react";
import type { ViewNode, GraphTransform } from "./graphModel";
const MIN_ZOOM = 0.18;
const MAX_ZOOM = 5.5;
function clamp(n: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, n));
}
export function useGraphViewport(phase: string, nodes: ViewNode[], positionsCount: number) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const interacted = useRef(false);
  const [viewport, setViewport] = useState({ w: 0, h: 0 });
  const [transform, setTransform] = useState<GraphTransform>({ x: 0, y: 0, k: 1 });
  const fitToGraph = useCallback(
    (force = false) => {
      if (
        (!force && interacted.current) ||
        nodes.length === 0 ||
        viewport.w === 0 ||
        viewport.h === 0
      )
        return;
      let minX = Infinity;
      let minY = Infinity;
      let maxX = -Infinity;
      let maxY = -Infinity;
      for (const node of nodes) {
        minX = Math.min(minX, node.x - node.r - 80);
        minY = Math.min(minY, node.y - node.r - 36);
        maxX = Math.max(maxX, node.x + node.r + (node.kind === "hypothesis" ? 230 : 80));
        maxY = Math.max(maxY, node.y + node.r + 36);
      }
      const pad = viewport.w < 760 ? 24 : 42;
      const k = clamp(
        Math.min(
          (viewport.w - pad * 2) / Math.max(1, maxX - minX),
          (viewport.h - pad * 2) / Math.max(1, maxY - minY),
        ),
        MIN_ZOOM,
        1.85,
      );
      setTransform({
        k,
        x: -((minX + maxX) / 2) * k,
        y: -((minY + maxY) / 2) * k,
      });
    },
    [nodes, viewport],
  );

  useEffect(() => {
    if (phase !== "ready") return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const resize = () => {
      const rect = canvas.getBoundingClientRect();
      setViewport({ w: rect.width, h: rect.height });
    };
    resize();
    const observer = new ResizeObserver(resize);
    observer.observe(canvas);
    return () => observer.disconnect();
  }, [phase]);

  useEffect(() => {
    if (positionsCount > 0) fitToGraph(false);
  }, [fitToGraph, positionsCount]);

  const hitTest = useCallback(
    (x: number, y: number) => {
      const rect = canvasRef.current?.getBoundingClientRect();
      const w = rect?.width ?? viewport.w;
      const h = rect?.height ?? viewport.h;
      let best: ViewNode | null = null;
      let bestDist = Infinity;
      for (const node of nodes) {
        const sx = w / 2 + transform.x + node.x * transform.k;
        const sy = h / 2 + transform.y + node.y * transform.k;
        const radius = Math.max(node.r * transform.k, node.kind === "hypothesis" ? 18 : 12);
        const dist = Math.hypot(x - sx, y - sy);
        if (dist <= radius && dist < bestDist) {
          best = node;
          bestDist = dist;
        }
      }
      return best;
    },
    [nodes, transform, viewport],
  );

  const zoomAtCanvasPoint = useCallback(
    (sx: number, sy: number, factor: number, w: number, h: number) => {
      interacted.current = true;
      setTransform((prev) => {
        const k = clamp(prev.k * factor, MIN_ZOOM, MAX_ZOOM);
        const wx = (sx - w / 2 - prev.x) / prev.k;
        const wy = (sy - h / 2 - prev.y) / prev.k;
        return { k, x: sx - w / 2 - wx * k, y: sy - h / 2 - wy * k };
      });
    },
    [],
  );

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const onNativeWheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = canvas.getBoundingClientRect();
      const sx = event.clientX - rect.left;
      const sy = event.clientY - rect.top;
      const amount = clamp(Math.abs(event.deltaY), 20, 260);
      const factor = event.deltaY < 0 ? 1 + amount / 520 : 1 / (1 + amount / 520);
      zoomAtCanvasPoint(sx, sy, factor, rect.width, rect.height);
    };
    canvas.addEventListener("wheel", onNativeWheel, { passive: false });
    return () => canvas.removeEventListener("wheel", onNativeWheel);
  }, [phase, zoomAtCanvasPoint]);

  const zoomBy = useCallback(
    (factor: number) => {
      const rect = canvasRef.current?.getBoundingClientRect();
      if (!rect) {
        setTransform((prev) => ({ ...prev, k: clamp(prev.k * factor, MIN_ZOOM, MAX_ZOOM) }));
        return;
      }
      zoomAtCanvasPoint(rect.width / 2, rect.height / 2, factor, rect.width, rect.height);
    },
    [zoomAtCanvasPoint],
  );

  return { canvasRef, interacted, viewport, transform, setTransform, fitToGraph, hitTest, zoomBy };
}
