import { expect, test } from "./fixture";

test("copy works on HTTP origins without the asynchronous Clipboard API", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", { value: undefined });
  });
  await page.goto("/search?chat=chat-1");
  await page.getByRole("button", { name: "Копировать", exact: true }).click();
  const followUp = page.getByRole("textbox", { name: "Уточняющий вопрос" });
  await followUp.focus();
  await followUp.press("ControlOrMeta+V");
  await expect(followUp).toHaveValue(/Термообработка/);
});

test("a denied copy explains recovery without claiming success", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: () => Promise.reject(new DOMException("Denied", "NotAllowedError")) },
    });
    document.execCommand = () => false;
  });
  await page.goto("/search?chat=chat-1");
  const copy = page.getByRole("button", { name: "Копировать", exact: true });
  await copy.click();
  await expect(page.getByText(/Браузер не разрешил копирование/)).toBeVisible();
  await expect(copy).toBeFocused();
  await expect(page.getByRole("button", { name: "Скопировано", exact: true })).toHaveCount(0);
});

test("the HTTP fallback keeps selection inside a dialog focus trap", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", { value: undefined });
  });
  await page.goto("/hypotheses");
  await page.getByRole("button", { name: "Экспорт", exact: true }).click();
  await page.getByRole("menuitem", { name: "Краткий отчёт", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Копировать", exact: true }).click();
  await page.keyboard.press("Escape");
  const search = page.getByRole("textbox", { name: "Поиск по гипотезам", exact: true });
  await search.focus();
  await search.press("ControlOrMeta+V");
  await expect(search).toHaveValue(/Термообработка/);
});
