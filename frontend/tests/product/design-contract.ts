import { expect, type Page } from "@playwright/test";

export async function inspectControlAlignment(page: Page) {
  const issues = await page.evaluate(() => {
    const selector =
      '[data-slot="button"], [data-slot="input"], [data-slot="select-trigger"], [data-slot="segmented"]';
    const groups = new Map<Element, Element[]>();
    for (const control of document.querySelectorAll<HTMLElement>(selector)) {
      if (!control.getClientRects().length || control.getBoundingClientRect().height === 0)
        continue;
      if (["inline", "content", "row"].includes(control.dataset.size ?? "")) continue;
      if (["absolute", "fixed"].includes(getComputedStyle(control).position)) continue;
      const group =
        control.closest('[data-slot="filter-bar"], [data-slot="dialog-footer"]') ??
        control.parentElement;
      if (!group || !/flex|grid/.test(getComputedStyle(group).display)) continue;
      groups.set(group, [...(groups.get(group) ?? []), control]);
    }
    return [...groups.values()]
      .filter((controls) => controls.length > 1)
      .map((controls) =>
        controls.map((control) => {
          const { height, top, bottom } = control.getBoundingClientRect();
          return {
            label: control.getAttribute("aria-label") ?? control.textContent?.trim().slice(0, 60),
            height,
            top,
            bottom,
            minimum: parseFloat(getComputedStyle(control).minHeight) || height,
          };
        }),
      )
      .filter((controls) => {
        const minimums = controls.map(({ minimum }) => minimum);
        if (Math.max(...minimums) - Math.min(...minimums) > 1) return true;
        // При увеличении текста отдельная строка может расти; соседи в одном ряду остаются равными.
        return controls.some((control, index) =>
          controls.slice(index + 1).some((other) => {
            const overlap =
              Math.min(control.bottom, other.bottom) - Math.max(control.top, other.top);
            return overlap > 1 && Math.abs(control.height - other.height) > 1;
          }),
        );
      });
  });
  expect
    .soft(issues, "Controls in the same action or filter group must have equal heights")
    .toEqual([]);
}

export async function inspectDesignContract(page: Page) {
  await inspectControlAlignment(page);
  const inspect = () =>
    page.evaluate(() => {
      const issues: string[] = [];
      const probe = document.createElement("div");
      probe.style.cssText =
        "position:fixed;visibility:hidden;pointer-events:none;transition:none!important";
      document.body.append(probe);
      const token = (property: string, value: string) => {
        probe.style.setProperty(property, value);
        return getComputedStyle(probe).getPropertyValue(property);
      };
      for (const title of document.querySelectorAll("h1")) {
        // Заголовок печатной версии на экране скрыт: прямоугольника у него нет.
        if (!title.getClientRects().length) continue;
        const style = getComputedStyle(title);
        if (title.getAttribute("data-slot") !== "page-title")
          issues.push("Heading bypasses PageTitle");
        if (style.fontFamily !== token("font-family", "var(--font-heading)"))
          issues.push("Heading font differs from the kit");
        const size =
          title.getAttribute("data-variant") === "welcome"
            ? window.matchMedia("(min-width: 768px)").matches
              ? "5xl"
              : "4xl"
            : "3xl";
        if (style.fontSize !== token("font-size", `var(--text-${size})`))
          issues.push("Heading size differs from the kit");
        const layout = title.closest('[data-slot="page-layout"]');
        if (
          layout &&
          Math.abs(
            title.getBoundingClientRect().left -
              layout.getBoundingClientRect().left -
              parseFloat(getComputedStyle(layout).paddingLeft),
          ) > 1
        )
          issues.push("Page heading differs from the shared left gutter");
      }
      for (const element of document.querySelectorAll("main *, header *, nav *")) {
        if (!element.getClientRects().length) continue;
        if (getComputedStyle(element).backgroundImage.includes("gradient("))
          issues.push(`Gradient outside the kit: ${element.tagName}`);
      }
      const fills: Record<string, string> = {
        default: "primary",
        outline: "background",
        secondary: "secondary",
        destructive: "destructive",
      };
      for (const control of document.querySelectorAll<HTMLElement>(
        '[data-slot="button"], [data-slot="input"], [data-slot="select-trigger"]',
      )) {
        if (!control.getClientRects().length || control.getBoundingClientRect().height === 0)
          continue;
        const style = getComputedStyle(control);
        const { slot, size, variant } = control.dataset;
        const label = `${control.tagName} ${control.textContent?.trim().slice(0, 40)}`;
        const selected =
          control.getAttribute("aria-pressed") === "true" ||
          control.getAttribute("aria-expanded") === "true";
        const fill =
          slot === "button"
            ? variant === "outline" && selected
              ? "brand-wash"
              : fills[variant ?? ""]
            : "background";
        if (fill && style.backgroundColor !== token("background-color", `var(--${fill})`))
          issues.push(`${label}: local background`);
        if (slot !== "button" && style.borderColor !== token("border-color", "var(--input)"))
          issues.push(`${label}: local field border`);
        const heightToken = { default: "md", icon: "md", sm: "sm", "icon-sm": "sm", lg: "lg" }[
          size ?? "default"
        ];
        if (heightToken) {
          const height = token("height", `var(--control-${heightToken})`);
          const actual =
            slot === "button" && !size?.startsWith("icon") ? style.minHeight : style.height;
          if (actual !== height) issues.push(`${label}: ${actual} instead of ${height}`);
        }
      }
      for (const bar of document.querySelectorAll<HTMLElement>('[data-slot="filter-bar"]')) {
        const height = token("height", `var(--control-${bar.dataset.size === "sm" ? "sm" : "md"})`);
        for (const control of bar.querySelectorAll<HTMLElement>(
          '[data-slot="input"], [data-slot="select-trigger"], [data-slot="button"], [data-slot="segmented"]',
        )) {
          if (!control.getClientRects().length) continue;
          if (["inline", "content", "row"].includes(control.dataset.size ?? "")) continue;
          const actual = getComputedStyle(control).height;
          if (actual !== height)
            issues.push(`FilterBar ${control.dataset.slot}: ${actual} instead of ${height}`);
        }
      }
      probe.remove();
      return issues;
    });
  expect
    .soft(await inspect(), "Every page must render shared typography, gutters and controls")
    .toEqual([]);
  await page.evaluate(() => {
    document.documentElement.style.setProperty("--control-md", "48px");
    document.documentElement.style.setProperty("--primary", "var(--brand)");
  });
  await expect
    .poll(inspect, { message: "Token changes must propagate without page-specific overrides" })
    .toEqual([]);
  await page.evaluate(() => {
    document.documentElement.style.removeProperty("--control-md");
    document.documentElement.style.removeProperty("--primary");
  });
}
