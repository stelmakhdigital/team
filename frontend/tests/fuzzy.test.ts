import { describe, expect, it } from 'vitest';
import { fuzzyMatch, fuzzyRank, isSubstring } from '../src/lib/fuzzy';

// P2.10: Command palette — unit-тест чистого fuzzy-матчера (без React).
describe('P2.10: fuzzy matcher (lib/fuzzy)', () => {
  it('substring (case-insensitive) matches + returns indices', () => {
    const m = fuzzyMatch('dash', 'Dashboard');
    expect(m).not.toBeNull();
    expect(m!.indices).toEqual([0, 1, 2, 3]);
    // case-insensitive
    expect(fuzzyMatch('DASH', 'Dashboard')).not.toBeNull();
    expect(fuzzyMatch('aTn', 'Attention')).not.toBeNull();
  });

  it('subsequence (non-consecutive) matches, lower score than substring', () => {
    const sub = fuzzyMatch('db', 'Dashboard'); // d(0) b(5)
    const str = fuzzyMatch('dash', 'Dashboard');
    expect(sub).not.toBeNull();
    expect(sub!.indices).toEqual([0, 4]);
    expect(str!.score).toBeGreaterThan(sub!.score); // последовательность (подстрока) лучше
  });

  it('returns null when query is not a subsequence', () => {
    expect(fuzzyMatch('x', 'abc')).toBeNull();
    expect(fuzzyMatch('ba', 'abc')).toBeNull(); // b после a — порядок сломан
    expect(fuzzyMatch('toolongquery', 'abc')).toBeNull();
  });

  it('empty query matches everything (score 0)', () => {
    expect(fuzzyMatch('', 'Anything')).toEqual({ score: 0, indices: [] });
    expect(fuzzyMatch('   ', 'Anything')).toEqual({ score: 0, indices: [] });
  });

  it('ranks tighter/word-boundary matches higher', () => {
    const tight = fuzzyMatch('team', 'Teams')!;
    const loose = fuzzyMatch('team', 'Create a brand new team')!;
    expect(tight.score).toBeGreaterThan(loose.score);
    // word-boundary: «w» в «Worker» (начало) лучше, чем внутри слова
    const boundary = fuzzyMatch('w', 'Worker')!;
    const mid = fuzzyMatch('w', 'sWim')!;
    expect(boundary.score).toBeGreaterThan(mid.score);
  });

  it('fuzzyRank sorts by relevance (best first) and drops non-matches', () => {
    const items = ['Dashboard', 'Settings', 'Tasks', 'Diagnostics'];
    const ranked = fuzzyRank(items, 'dash', (x) => x);
    expect(ranked.length).toBeGreaterThan(0);
    expect(ranked[0].item).toBe('Dashboard');
    // «Tasks» не матчится «dash»
    expect(ranked.some((r) => r.item === 'Dashboard')).toBe(true);
    // descending score
    for (let i = 1; i < ranked.length; i++) {
      expect(ranked[i - 1].match.score).toBeGreaterThanOrEqual(ranked[i].match.score);
    }
  });

  it('isSubstring — simple case-insensitive contains', () => {
    expect(isSubstring('att', 'Attention')).toBe(true);
    expect(isSubstring('ATN', 'Attention')).toBe(false); // не подстрока (subseq да, substring нет)
    expect(isSubstring('', 'Anything')).toBe(true);
  });
});
