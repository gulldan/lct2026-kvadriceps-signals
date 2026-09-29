export function inferDirection(
  title: string,
  baseline: number | null,
  target: number | null,
): string {
  if (baseline !== null && target !== null) {
    if (target > baseline) return "increase";
    if (target < baseline) return "decrease";
    return "maintain";
  }
  const text = title.toLowerCase();
  if (/(сниз|уменьш|сократ|минимиз|удешев|lower|reduce|decreas|minimi[sz]e)/i.test(text)) {
    return "decrease";
  }
  if (/(повыс|увелич|улучш|максимиз|рост|increase|improv|maximi[sz]e|raise)/i.test(text)) {
    return "increase";
  }
  return "maintain";
}
