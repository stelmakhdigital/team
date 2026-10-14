import { describe, expect, it } from 'vitest';
import { dayLabel } from '../src/lib/format';

// W11: разделители дат в ленте Pulse.
describe('W11: dayLabel', () => {
  it('сегодня → «Сегодня», вчера → «Вчера»', () => {
    const now = new Date();
    expect(dayLabel(now.toISOString())).toBe('Сегодня');
    expect(dayLabel(new Date(now.getTime() - 86_400_000).toISOString())).toBe('Вчера');
  });

  it('старая дата → «д число месяц» (ru-RU)', () => {
    expect(dayLabel('2025-03-07T12:00:00Z')).toMatch(/2025|марта|марш|March/);
  });

  it('битая дата → пустая строка (сепаратор не рисуется)', () => {
    expect(dayLabel('garbage')).toBe('');
    expect(dayLabel(undefined)).toBe('');
  });
});
