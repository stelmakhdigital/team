import { useEffect, useRef } from 'react';

// W3.1: единая «стек»-семантика Esc — закрывается только верхний открытый
// оверлей (модалки/палитры, открытые последним). Компонент, который уже ловит
// Esc сам (в input), дублировать не нужно — capture-слушатель срабатывает
// раньше и гасит событие (stopPropagation).
// Модульный стек (по id): верхний элемент получает Esc.

const overlayStack: symbol[] = [];

export function useEscapeStack(onEscape: () => void, active: boolean): void {
  const idRef = useRef<symbol | null>(null);
  if (idRef.current === null) idRef.current = Symbol('overlay');
  const id = idRef.current;
  const cbRef = useRef(onEscape);
  cbRef.current = onEscape;

  useEffect(() => {
    if (!active) return;
    overlayStack.push(id);
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (overlayStack[overlayStack.length - 1] !== id) return; // не верхний
      e.preventDefault();
      e.stopPropagation();
      cbRef.current();
    };
    window.addEventListener('keydown', onKey, true); // capture — до input-обработчиков
    return () => {
      const i = overlayStack.indexOf(id);
      if (i >= 0) overlayStack.splice(i, 1);
      window.removeEventListener('keydown', onKey, true);
    };
  }, [active, id]);
}
