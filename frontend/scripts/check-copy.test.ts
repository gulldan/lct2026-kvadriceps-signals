import { expect, test } from "bun:test";

import { checkCopy } from "./check-copy";

test("check-copy находит латиницу, проценты, дефисы, многоточие и кавычки", () => {
  const errors = checkCopy(
    "t",
    new Map([
      ["latin", "Разметку выполнил Codex"],
      ["percent", "Рост 10%"],
      ["range", "За 2020-2025 годы"],
      ["ellipsis", "Ждите..."],
      ["quotes", 'Раздел "Как считается"'],
      ["ok", "Обновлено 10 % · 2020–2025 · «arXiv» и GitHub"],
    ]),
  );
  expect(errors.map((e) => e.split("\t")[0])).toEqual([
    "t:latin",
    "t:percent",
    "t:range",
    "t:ellipsis",
    "t:quotes",
  ]);
});
