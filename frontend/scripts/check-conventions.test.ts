import { expect, test } from "bun:test";
import { validateImports } from "./check-conventions";

test("layer boundaries cover aliases, relative paths, re-exports and lazy imports without matching comments", () => {
  const feature = "src/features/search/pages/SearchHome.tsx";
  for (const source of [
    'import { auth } from "@/features/auth/api";',
    'export { auth } from "../../auth/api";',
    'export * from "../../auth/api";',
    'const view = import("@/features/auth/pages/Login");',
    'const view = require("@/features/auth/pages/Login");',
    'import { App } from "@/app/App";',
  ])
    expect(validateImports(feature, source)).toHaveLength(1);
  expect(validateImports(feature, 'import { auth } from "@/features/auth";')).toEqual([]);
  expect(validateImports(feature, 'import { query } from "../api";')).toEqual([]);
  expect(validateImports(feature, '// import { auth } from "@/features/auth/api";')).toEqual([]);
  expect(
    validateImports("src/shared/ui/Button.tsx", 'export * from "../../features/auth";'),
  ).toHaveLength(1);
});
