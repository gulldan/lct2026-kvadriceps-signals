export function readColorToken(name: string, element: Element): string {
  const value = getComputedStyle(element).getPropertyValue(`--${name}`).trim();
  if (!value) throw new Error(`Missing design token: --${name}`);
  return value;
}

/** Word требует RGB в hex без «#»; цвета печати не зависят от темы экрана. */
export const printColors = {
  ink: "0A0A0B",
  muted: "6F6F6F",
  brand: "2B61EC",
  border: "DFDFDF",
  strongBorder: "808082",
  headingWash: "F5F5F5",
} as const;
