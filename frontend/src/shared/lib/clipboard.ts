/** Работает и в HTTP-развёртывании в локальной сети, где Clipboard API нет. */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Браузер может запретить асинхронный API, но разрешить копирование выделения.
  }

  const focused = document.activeElement;
  const field = document.createElement("textarea");
  field.value = text;
  field.readOnly = true;
  field.className = "sr-only";
  // Выделение остаётся внутри ловушки фокуса модального окна, если копируют из его действий.
  const dialog = focused instanceof Element ? focused.closest('[role="dialog"]') : null;
  (dialog ?? document.body).append(field);
  field.select();
  try {
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    field.remove();
    if (focused instanceof HTMLElement) focused.focus({ preventScroll: true });
  }
}
