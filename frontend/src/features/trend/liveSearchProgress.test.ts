import { expect, test } from "bun:test";
import { libraryProgress } from "./liveSearchProgress";
import type { LiveSearchState } from "./liveApi";

test("library follows reported stages without inventing completed sources", () => {
  const state: LiveSearchState = {
    query: "energy",
    status: "running",
    step: "patents",
    elapsed_seconds: 80,
    visited_steps: ["arxiv", "patents", "web"],
  };
  const progress = libraryProgress(state, false, false);
  expect(progress.activity).toBe("collect");
  expect(progress.shelf).toBe("world");
  expect([...progress.visited]).toEqual(["arxiv", "patents", "web"]);
  expect(libraryProgress({ ...state, visited_steps: undefined }, false, false).visited).toEqual(
    new Set(["patents"]),
  );
  expect(libraryProgress(state, true, false).activity).toBe("wait");
  expect(libraryProgress(state, true, true).activity).toBe("reconnect");
  expect(libraryProgress({ ...state, step: "analysis" }, false, false).activity).toBe("analyze");
  expect(libraryProgress({ ...state, step: "cards" }, false, false).activity).toBe("write");
  expect(libraryProgress({ ...state, step: undefined }, false, false).activity).toBe("prepare");
});
