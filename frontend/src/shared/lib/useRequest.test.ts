import { describe, expect, it } from "bun:test";

import { retryDelay } from "./useRequest";

describe("useRequest при сбое", () => {
  it("держит обычный шаг, пока ответы приходят", () => {
    expect(retryDelay(2000, 0)).toBe(2000);
  });

  it("разводит повторы после отказов и упирается в потолок", () => {
    expect(retryDelay(2000, 1)).toBe(4000);
    expect(retryDelay(2000, 2)).toBe(8000);
    expect(retryDelay(2000, 10)).toBe(30_000);
  });
});
