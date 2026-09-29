import { expect, test } from "bun:test";
import type { GraphEdge, GraphNode } from "../api";
import { buildNeighbors, toViewData } from "./graphModel";

test("graph keeps mixed evidence and bounds documents without dangling links", () => {
  const nodes: GraphNode[] = [{ id: "h", kind: "hypothesis", label: "Hypothesis", kpi_id: "k" }];
  const edges: GraphEdge[] = [];
  for (let i = 0; i < 250; i++) {
    nodes.push({ id: `d${i}`, kind: "document", label: `Evidence ${i}` });
    edges.push({ source: "h", target: `d${i}`, class: "supports", chunk_id: `c${i}` });
  }
  edges.push({ source: "h", target: "d0", class: "contradicts", chunk_id: "counter" });
  const view = toViewData(nodes, edges, "k", "all", "");
  expect(view.nodes).toHaveLength(161);
  expect(view.droppedDocs).toBe(90);
  expect(view.links.find((link) => link.target === "d0")).toMatchObject({
    class: "mixed",
    chunks: 2,
  });
  const ids = new Set(view.nodes.map((node) => node.id));
  expect(view.links.every((link) => ids.has(link.source) && ids.has(link.target))).toBe(true);
  expect(buildNeighbors("d0", view.links)).toEqual(new Set(["d0", "h"]));
  const searched = toViewData(nodes, edges, "k", "all", "Hypothesis");
  expect(searched.nodes).toHaveLength(241);
  expect(toViewData(nodes, edges, "foreign", "all", "").nodes).toHaveLength(0);
});
