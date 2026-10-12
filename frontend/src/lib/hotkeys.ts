// W3.1: общий guard — «фокус не в поле ввода» (input/textarea/select/contenteditable).
export function isEditableTarget(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false;
  const tag = t.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || t.isContentEditable;
}

// W3.1: есть ли открытый оверлей-исключение (CommandPalette/SendToSession/Preview/…)
// → глобальные hotkeys (K, ?) не срабатывают, пока он открыт.
export function hasExcludedOverlay(): boolean {
  return document.querySelector('[data-hotkey-exclude]') !== null;
}
