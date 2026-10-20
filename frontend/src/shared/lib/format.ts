import { currentLocale, i18n } from "@/shared/i18n";

export function formatNumber(value: number, options: Intl.NumberFormatOptions = {}): string {
  return Number.isFinite(value)
    ? new Intl.NumberFormat(currentLocale(), options).format(value)
    : "—";
}

export function formatPercent(share: number, digits = 0): string {
  return `${formatNumber(share * 100, { maximumFractionDigits: digits })} %`;
}

export function dateFormatter(
  options: Intl.DateTimeFormatOptions,
  locale = currentLocale(),
): Intl.DateTimeFormat {
  return new Intl.DateTimeFormat(locale, options);
}

const SIZE_UNIT_KEYS = ["bytes", "kb", "mb", "gb", "tb"] as const;

export function humanSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < SIZE_UNIT_KEYS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  // Цикл не выпускает unit за границы массива; «??» — только для типов.
  const unitKey = SIZE_UNIT_KEYS[unit] ?? "bytes";
  const digits = value >= 100 || unit === 0 ? 0 : 1;
  return `${value.toFixed(digits)} ${i18n.t(`units.${unitKey}`)}`;
}

export function formatDate(iso: string): string {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return "—";
  return d.toLocaleDateString(currentLocale(), {
    day: "numeric",
    month: "long",
    year: "numeric",
  });
}

export function formatMonth(period: string): string {
  if (!period) return "—";
  const d = new Date(`${period}-01`.slice(0, 10));
  // Старые снимки радара хранят кварталы («2026-Q3»): прочерк вместо них потерял бы данные.
  if (Number.isNaN(d.getTime())) return period;
  return d.toLocaleDateString(currentLocale(), { month: "long", year: "numeric", timeZone: "UTC" });
}

export function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString(currentLocale(), {
    day: "numeric",
    month: "long",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** «1 год 1 месяц назад» в узкой колонке рвался после «1» и читался как перечисление чисел. */
const together = (value: string) => value.replaceAll(" ", "\u00a0");

/** Будущая или неразборчивая дата → «—»: «через …» было бы ошибкой данных. */
export function formatElapsed(iso: string, now = Date.now()): string {
  const then = new Date(iso.length === 7 ? `${iso}-01` : iso);
  if (!iso || Number.isNaN(then.getTime()) || then.getTime() > now) return "—";
  const today = new Date(now);
  let years = today.getUTCFullYear() - then.getUTCFullYear();
  let months = today.getUTCMonth() - then.getUTCMonth();
  if (today.getUTCDate() < then.getUTCDate()) months -= 1;
  if (months < 0) {
    years -= 1;
    months += 12;
  }
  if (years === 0 && months === 0) {
    const days = Math.floor((now - then.getTime()) / 86_400_000);
    return days === 0
      ? i18n.t("units.elapsed.today")
      : i18n.t("units.elapsed.ago", {
          value: together(i18n.t("units.elapsed.days", { count: days })),
        });
  }
  const parts = [
    years > 0 ? together(i18n.t("units.elapsed.years", { count: years })) : "",
    months > 0 ? together(i18n.t("units.elapsed.months", { count: months })) : "",
  ].filter(Boolean);
  return i18n.t("units.elapsed.ago", { value: parts.join(" ") });
}

export function formatTime(iso: string): string {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return "—";
  return dateFormatter({ hour: "2-digit", minute: "2-digit" }).format(d);
}

export function formatDateShort(iso: string): string {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return "—";
  return d.toLocaleDateString(currentLocale());
}

// «мРНК», «iPhone», «eVTOL»: строчная перед заглавной — часть написания, а не начало фразы.
export function upperFirst(text: string): string {
  return /^\p{Ll}\p{Lu}/u.test(text) ? text : text.charAt(0).toLocaleUpperCase() + text.slice(1);
}

// Словарь ICU пишет русские названия без «ё», а интерфейс её сохраняет.
export function regionNames(language: string): (code: string) => string {
  const names = new Intl.DisplayNames(language, { type: "region" });
  return (code) => (names.of(code) ?? code).replace(/(Соедин|Объедин)ен/g, "$1ён");
}
