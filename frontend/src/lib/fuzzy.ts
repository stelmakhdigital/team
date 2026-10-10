// P2.10: Command palette — fuzzy-матчинг (чистые функции, без React — unit-тестируемо).
// Минимум — подстрока (case-insensitive); ранжирование — субпоследовательность (subseq):
// ранжирование по очкам (consecutive + word-boundary + короче-цель лучше).

export interface FuzzyResult {
  score: number; // выше = лучше
  indices: number[]; // позиции совпавших символов в target
}

const BOUNDARY = /[\s._\-/:]/;

/**
 * Субпоследовательность-матч (case-insensitive). Подстрока — частный случай
 * (последовательные совпадения получают бонус). Возвращает null, если query
 * не находится в target в порядке. Пустой query → матч со score 0 (всё подходит).
 */
export function fuzzyMatch(query: string, target: string): FuzzyResult | null {
  const q = query.trim().toLowerCase();
  const t = target.toLowerCase();
  if (q.length === 0) return { score: 0, indices: [] };
  if (q.length > t.length) return null;
  let qi = 0;
  let score = 0;
  let prev = -2;
  const indices: number[] = [];
  for (let i = 0; i < t.length && qi < q.length; i++) {
    if (t[i] !== q[qi]) continue;
    indices.push(i);
    score += 1; // base за символ
    if (i === prev + 1) score += 4; // consecutive (подстрока-бонус)
    if (i === 0 || BOUNDARY.test(target[i - 1])) score += 8; // word-boundary
    prev = i;
    qi++;
  }
  if (qi < q.length) return null; // не все символы найдены в порядке
  score -= Math.floor(t.length / 10); // короче-цель (точнее) чуть лучше
  return { score, indices };
}

/** Подстрока (case-insensitive) — простой содержит-ли. */
export function isSubstring(query: string, target: string): boolean {
  if (!query.trim()) return true;
  return target.toLowerCase().includes(query.trim().toLowerCase());
}

/**
 * Ранжирование: отбирает матчи (query vs label(item)) + сортирует по score desc.
 * Чистая функция — unit-тестируема.
 */
export function fuzzyRank<T>(
  items: T[],
  query: string,
  label: (item: T) => string,
): { item: T; match: FuzzyResult }[] {
  const scored: { item: T; match: FuzzyResult }[] = [];
  for (const item of items) {
    const match = fuzzyMatch(query, label(item));
    if (match !== null) scored.push({ item, match });
  }
  scored.sort((a, b) => b.match.score - a.match.score);
  return scored;
}

/** Разбивает label на части для подсветки: matched (в indices) → true. */
export function highlightParts(text: string, indices: number[]): { char: string; hit: boolean }[] {
  const set = new Set(indices);
  const out: { char: string; hit: boolean }[] = [];
  for (let i = 0; i < text.length; i++) out.push({ char: text[i], hit: set.has(i) });
  return out;
}
