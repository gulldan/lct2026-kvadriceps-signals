const LABELS: Record<string, string> = { arxiv: "arXiv", github: "GitHub" };

export function providerLabel(name: string): string {
  return LABELS[name] ?? name;
}

export function originOf(url: string, provider: string): string {
  try {
    const owner = new URL(url).pathname.split("/").find(Boolean);
    return provider === "github" && owner ? owner : providerLabel(provider);
  } catch {
    return providerLabel(provider);
  }
}
