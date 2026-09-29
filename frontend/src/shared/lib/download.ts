/** Ссылку отзываем не сразу: часть браузеров ещё не начала читать blob и молча теряла загрузку. */
export function saveFile(filename: string, content: string | Blob, mime?: string): void {
  const blob = content instanceof Blob ? content : new Blob([content], { type: mime });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}
