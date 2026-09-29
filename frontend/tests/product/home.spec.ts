import { expect, test } from "./fixture";

for (const path of ["/search", "/search?q=%20%20"]) {
  test(`${path} offers source topics without loading personal search history`, async ({
    page,
    api,
  }) => {
    await page.goto(path);
    const focus = page.getByRole("region", { name: "Технологии в фокусе" });
    await expect(focus.getByRole("link")).toHaveCount(2);
    await expect(page.getByRole("button", { name: "Найти", exact: true })).toBeDisabled();
    await expect(page.getByText("Недавние запросы", { exact: true })).toBeHidden();
    await expect(
      page.getByText("Как повысить термостойкость композита?", { exact: true }),
    ).toBeHidden();
    expect(api.calls.filter((call) => call.path.startsWith("/chats"))).toEqual([]);

    const topic = focus.getByRole("link", { name: "Полимерные композиты", exact: true });
    await expect(topic).toHaveAttribute("href", "/clusters/cluster-1");
    await topic.focus();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(/\/clusters\/cluster-1$/);
    await expect(
      page.getByRole("heading", { name: "Полимерные композиты", exact: true }),
    ).toBeVisible();
    expect(
      api.calls.filter((call) => call.method === "POST" && call.path.startsWith("/chats")),
    ).toEqual([]);
  });
}

test("topic suggestions skip unusable labels and change after every search", async ({
  page,
  api,
}) => {
  const base = api.state.clusters[0]!;
  const usable = [
    "Флотация руд",
    "Защитные покрытия",
    "Углеродные волокна",
    "Керамические материалы",
    "Скоринг кредитных заявок",
    "Цифровые двойники печей",
    "Антифрод в платежах",
    "Водородная энергетика",
    "Предиктивное обслуживание",
    "Порошковая металлургия",
    "Обогащение угля",
    "Промышленная робототехника",
  ];
  api.state.clusters = [
    ...usable.map((label, index) => ({ label, document_count: 40 - index })),
    { label: "Corporate Source / Research Department", document_count: 90 },
    { label: "Архивная тема", document_count: 80, status: "archived" },
    { label: "<metadata>", document_count: 70 },
    { label: "тематическая группа", document_count: 60 },
    { label: "ФЛОТАЦИЯ РУД", document_count: 19 },
    { label: "Одиночная публикация", document_count: 1 },
  ].map((topic, index) => Object.assign({}, base, topic, { id: `focus-${index}` }));
  const links = page.getByRole("region", { name: "Технологии в фокусе" }).getByRole("link");
  await page.goto("/search");
  await expect(links).toHaveCount(6);
  const first = await links.allTextContents();
  await page
    .getByRole("textbox", { name: "Поисковый запрос" })
    .fill("Как повысить прочность материала?");
  await page.getByRole("button", { name: "Найти", exact: true }).click();
  await expect(page).toHaveURL(/\/search\?chat=/);
  await page.getByRole("link", { name: "Поиск по документам", exact: true }).click();
  await expect(links).toHaveCount(6);
  const second = await links.allTextContents();
  expect([...first, ...second].toSorted()).toEqual(usable.toSorted());
});

for (const state of ["empty", "unavailable"] as const) {
  test(`search remains usable when topics are ${state}`, async ({ page, api }) => {
    if (state === "empty") api.state.clusters = [];
    else api.failures.set("GET /clusters", 500);
    await page.goto("/search");
    await expect.poll(() => api.calls.some((call) => call.path === "/clusters")).toBe(true);
    await expect(page.getByRole("region", { name: "Технологии в фокусе" })).toBeHidden();
    await page
      .getByRole("textbox", { name: "Поисковый запрос" })
      .fill("Как повысить прочность материала?");
    await page.getByRole("button", { name: "Найти", exact: true }).click();
    await expect(page).toHaveURL(/\/search\?chat=/);
    await expect(
      page.getByRole("heading", { name: "Как повысить прочность материала?", exact: true }),
    ).toBeVisible();
  });
}
