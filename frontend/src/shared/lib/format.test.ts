import { beforeAll, describe, expect, test } from "bun:test";

import { initI18n } from "@/shared/i18n";

import { formatDate, formatElapsed, humanSize } from "./format";

beforeAll(async () => {
  // В bun test нет DOM; initI18n выставляет document.documentElement.lang.
  const documentDescriptor = Object.getOwnPropertyDescriptor(globalThis, "document");
  (globalThis as { document?: unknown }).document ??= { documentElement: { lang: "" } };
  try {
    await initI18n();
  } finally {
    // Другие тесты грузят компоненты, которые проверяют наличие настоящего документа.
    if (documentDescriptor) Object.defineProperty(globalThis, "document", documentDescriptor);
    else Reflect.deleteProperty(globalThis, "document");
  }
});

describe("humanSize", () => {
  test("байты и переходы единиц", () => {
    expect(humanSize(0)).toBe("0 Б");
    expect(humanSize(1093)).toBe("1.1 КБ");
    expect(humanSize(5 * 1024 * 1024)).toBe("5.0 МБ");
    expect(humanSize(120 * 1024)).toBe("120 КБ"); // >=100 — без дробной части
  });

  test("мусор на входе — прочерк", () => {
    expect(humanSize(-1)).toBe("—");
    expect(humanSize(Number.NaN)).toBe("—");
  });
});

describe("formatDate", () => {
  test("пустая или битая строка — прочерк", () => {
    expect(formatDate("")).toBe("—");
    expect(formatDate("не дата")).toBe("—");
  });

  test("валидная дата локализуется", () => {
    expect(formatDate("2026-05-28T10:00:00Z")).toContain("2026");
  });
});

describe("formatElapsed", () => {
  const now = Date.parse("2026-09-22T12:00:00Z");
  // Неразрывный пробел внутри «1 год» в ожидании нечитаем: сравниваем текст.
  const elapsed = (iso: string) => formatElapsed(iso, now).replaceAll("\u00a0", " ");
  test("считает годы и месяцы от одного «сегодня» при любой точности даты", () => {
    expect(elapsed("2025-08-01")).toBe("1 год 1 месяц назад");
    expect(elapsed("2026-08")).toBe("1 месяц назад");
    expect(elapsed("2026-09-21T03:34:08Z")).toBe("1 день назад");
    expect(elapsed("2026-09-22T03:34:08Z")).toBe("сегодня");
    expect(elapsed("2024-09-22")).toBe("2 года назад");
    expect(formatElapsed("2025-08-01", now)).toContain("\u00a0");
  });
  test("будущая и пустая дата — не отсчёт", () => {
    expect(formatElapsed("2027-01-01", now)).toBe("—");
    expect(formatElapsed("", now)).toBe("—");
  });
});
