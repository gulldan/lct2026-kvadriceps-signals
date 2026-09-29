import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import RichTextInner from "../ui/RichTextInner";

test("цены остаются текстом рядом с формулами, кодом и экранированными долларами", () => {
  for (const [text, prose] of [
    [
      "Стоимость $0,33 за вызов и $0.33 за запрос. Формула $2x + 1$.",
      "Стоимость $0,33 за вызов и $0.33 за запрос.",
    ],
    [
      "Cost $0.33 per call and $0.33 per request. Formula $2x + 1$.",
      "Cost $0.33 per call and $0.33 per request.",
    ],
    ["Цена $0,33 за запрос. Formula $2x + 1$; additional cost $0.65.", "Цена $0,33 за запрос."],
  ] as const) {
    const html = renderToStaticMarkup(<RichTextInner>{text}</RichTextInner>);
    expect(html).toContain(prose);
    expect(html.match(/class="katex"/g)).toHaveLength(1);
    expect(html).toContain("2x + 1");
  }
  const protectedText = renderToStaticMarkup(
    <RichTextInner>
      {"`$0.33 and $0.65` — \\$5 и \\$10. Формула $$ x^2 $$; химия $\\ce{H2O}$."}
    </RichTextInner>,
  );
  expect(protectedText).toContain("<code>$0.33 and $0.65</code>");
  expect(protectedText).toContain("$5 и $10.");
  expect(protectedText.match(/class="katex"/g)).toHaveLength(2);
  const spacedMath = renderToStaticMarkup(
    <RichTextInner>
      {"Cost $0.33; formulas $2x + 1 $, $2 x $, $2 + 2 $ and $$ 2 x $$; cost $0.65."}
    </RichTextInner>,
  );
  expect(spacedMath).toContain("Cost $0.33; formulas");
  expect(spacedMath).toContain("cost $0.65.");
  expect(spacedMath.match(/class="katex"/g)).toHaveLength(4);
  const followingMath = renderToStaticMarkup(
    <RichTextInner>{"Cost $0.33. Formula $x$; цена $0,65; формула $y$."}</RichTextInner>,
  );
  expect(followingMath).toContain("Cost $0.33. Formula");
  expect(followingMath).toContain("цена $0,65; формула");
  expect(followingMath.match(/class="katex"/g)).toHaveLength(2);
});
