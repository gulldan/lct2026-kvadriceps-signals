import { expect, test } from "bun:test";
import { createInstance } from "i18next";
import en from "@/shared/i18n/locales/en/trend.json";
import ru from "@/shared/i18n/locales/ru/trend.json";
import zh from "@/shared/i18n/locales/zh/trend.json";
import { passportLine, sourceGenreLabel } from "./signalSources";

test("investor genres remain readable in passports and exports in every locale", async () => {
  const i18n = createInstance();
  await i18n.init({
    resources: { ru: { trend: ru }, en: { trend: en }, zh: { trend: zh } },
    defaultNS: "trend",
  });
  for (const [language, resource] of Object.entries({ ru, en, zh })) {
    const t = i18n.getFixedT(language, "trend");
    for (const genre of ["investment_thesis", "investor_commentary"] as const) {
      expect(sourceGenreLabel(genre, t)).toBe(resource.source.genre[genre]);
      expect(passportLine("", { genre }, t)).toContain(resource.source.genre[genre]);
    }
    expect(sourceGenreLabel("future_unknown_genre", t)).toBe(resource.source.genre.other);
  }
});
