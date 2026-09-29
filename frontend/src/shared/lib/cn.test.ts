import { expect, test } from "bun:test";
import { cn } from "./cn";

test("semantic type sizes survive color composition and obey size overrides", () => {
  expect(cn("text-caption", "text-muted-foreground")).toBe("text-caption text-muted-foreground");
  expect(cn("text-caption", "text-sm")).toBe("text-sm");
  expect(cn("text-micro", "text-risk")).toBe("text-micro text-risk");
  expect(cn("h-control", "h-control-sm")).toBe("h-control-sm");
});
