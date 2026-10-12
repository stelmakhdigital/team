import { describe, expect, it } from 'vitest';
import appShellSrc from '../src/components/layout/AppShell.tsx?raw';

// W1.4: ноль emoji-иконок в кнопках/действиях.
// Сканируем все .tsx в src: каждый блок <button …>…</button> не должен содержать
// emoji-кодпоинты (U+1F000–U+1FAFF; U+2600–U+27BF с emoji-презентацией без U+FE0E)
// и не должен содержать U+FE0F (принудительная emoji-презентация).
// Единый монохромный стиль глифов — как в NAV (AppShell.tsx).

// все .tsx из src как raw-строки (vitest: Vite import.meta.glob)
const tsxFiles = import.meta.glob('../src/**/*.tsx', { as: 'raw', eager: true }) as Record<string, string>;

// U+2600–U+27BF: НЕ весь диапазон — там и текстовые глифы (✓ ✕ ✎ ☰ ⚙ и т.п.),
// которые разрешены (единый монохромный стиль). Флаггуем только реально
// emoji-кодпоинты этого блока (default emoji presentation).
const EMOJI_IN_2600_27BF = new Set([
  0x2600, 0x2601, 0x2602, 0x2603, 0x2604, 0x2605, 0x2606, 0x2609, 0x260a, 0x260b, 0x260c,
  0x260d, 0x260e, 0x260f, 0x2610, 0x2611, 0x2612, 0x2613, 0x2614, 0x2615, 0x2618, 0x261d,
  0x2620, 0x2622, 0x2623, 0x2624, 0x2626, 0x2627, 0x2628, 0x2629, 0x262a, 0x262b, 0x262c,
  0x262d, 0x262e, 0x262f, 0x2638, 0x2639, 0x263a, 0x263b, 0x263c, 0x2640, 0x2641,
  0x2642, 0x2648, 0x2649, 0x264a, 0x264b, 0x264c, 0x264d, 0x264e, 0x2648, 0x2653,
  0x265f, 0x2660, 0x2663, 0x2665, 0x2666, 0x2668, 0x267b, 0x267e, 0x267f, 0x2680, 0x2681,
  0x2682, 0x2683, 0x2684, 0x2685, 0x2686, 0x2687, 0x2688, 0x2689, 0x268a, 0x268b, 0x268c,
  0x268d, 0x268e, 0x268f, 0x2690, 0x2691, 0x2692, 0x2693, 0x2694, 0x2695, 0x2696, 0x2697,
  0x2698, 0x2699, 0x269a, 0x269b, 0x269c, 0x269d, 0x269f, 0x26a0, 0x26a1, 0x26a7, 0x26aa,
  0x26ab, 0x26b0, 0x26b1, 0x26b2, 0x26bd, 0x26be, 0x26c4, 0x26c5, 0x26c8, 0x26ce, 0x26cf,
  0x26d1, 0x26d3, 0x26d4, 0x26d5, 0x26e2, 0x26e4, 0x26e9, 0x26ea, 0x26f0, 0x26f1, 0x26f2,
  0x26f3, 0x26f4, 0x26f5, 0x26f6, 0x26f7, 0x26f8, 0x26f9, 0x26fa, 0x26fb, 0x26fc, 0x26fd,
  0x2702, 0x2703, 0x2704, 0x2705, 0x2708, 0x2709, 0x270a, 0x270b, 0x270c, 0x270d, 0x270f,
  0x2712, 0x2714, 0x2716, 0x271d, 0x2721, 0x2728, 0x2729, 0x272a, 0x272b, 0x272c, 0x2733,
  0x2734, 0x2744, 0x2747, 0x274c, 0x274e,
]);

// emoji-кодпоинт, который в UI должен рендериться как emoji (не текстовый глиф)
function isEmojiCp(cp: number, next: string | undefined): boolean {
  if (cp >= 0x1f000 && cp <= 0x1faff) return true; // emoji-only range
  if (cp >= 0x2600 && cp <= 0x27bf) {
    // dual presentation: с U+FE0E — текстовая презентация → разрешаем
    if (next === '\uFE0E') return false;
    return EMOJI_IN_2600_27BF.has(cp);
  }
  return false;
}

function buttonBlocks(): Array<{ file: string; block: string }> {
  const blocks: Array<{ file: string; block: string }> = [];
  for (const [file, text] of Object.entries(tsxFiles)) {
    // нормализуем JS-эскейпы \uFE0E / \uFE0F в реальные символы (строки в коде)
    const norm = text.replace(/\\uFE0E/g, '\uFE0E').replace(/\\uFE0F/gi, '\uFE0F');
    const re = /<button[\s\S]*?<\/button>/g;
    let m: RegExpExecArray | null;
    while ((m = re.exec(norm))) {
      blocks.push({ file: file.replace('../src/', ''), block: m[0] });
    }
  }
  return blocks;
}

describe('W1.4 монохромные кнопки: ноль emoji-иконок в кнопках', () => {
  it('в <button> нет emoji-кодпоинтов (текстовые глифы, FE0E для dual)', () => {
    const offenders: string[] = [];
    for (const { file, block } of buttonBlocks()) {
      const cps = Array.from(block);
      for (let i = 0; i < cps.length; i++) {
        const cp = cps[i].codePointAt(0)!;
        const next = i + 1 < cps.length ? cps[i + 1] : undefined;
        if (cp === 0xfe0f) {
          offenders.push(`${file}: U+FE0F (emoji-presentation) в кнопке: ${JSON.stringify(block.slice(0, 60))}`);
          break;
        }
        if (isEmojiCp(cp, next)) {
          offenders.push(`${file}: emoji U+${cp.toString(16).toUpperCase()} в кнопке: ${JSON.stringify(cps.slice(Math.max(0, i - 12), i + 12).join(''))}`);
          break;
        }
      }
    }
    expect(offenders).toEqual([]);
  });

  it('NAV (AppShell) — монохромный эталон, не изменён: ⬡/▣/☰/◉ на месте', () => {
    expect(appShellSrc).toContain("icon: '⬡'");
    expect(appShellSrc).toContain("icon: '▣'");
    expect(appShellSrc).toContain("icon: '☰'");
    expect(appShellSrc).toContain("icon: '◉'");
  });
});
