import { describe, expect, it } from 'vitest';
import { codeFromStatus, parseErrorBody } from '../src/api/errors';

describe('codeFromStatus', () => {
  it('maps common statuses', () => {
    expect(codeFromStatus(0)).toBe('network');
    expect(codeFromStatus(401)).toBe('unauthorized');
    expect(codeFromStatus(403)).toBe('forbidden');
    expect(codeFromStatus(404)).toBe('not_found');
    expect(codeFromStatus(409)).toBe('conflict');
    expect(codeFromStatus(422)).toBe('validation');
    expect(codeFromStatus(500)).toBe('server');
  });
});

describe('parseErrorBody (error model TBD, flex parsing)', () => {
  it('parses { error: { code, message } }', () => {
    expect(parseErrorBody({ error: { code: 'conflict', message: 'busy' } }, 409)).toEqual({
      message: 'busy',
      code: 'conflict',
      details: undefined,
    });
  });
  it('parses { message }', () => {
    expect(parseErrorBody({ message: 'nope' }, 404)).toEqual({ message: 'nope', code: 'not_found', details: undefined });
  });
  it('parses plain text', () => {
    expect(parseErrorBody('internal', 500)).toEqual({ message: 'internal', code: 'server', details: undefined });
  });
  it('falls back for empty body', () => {
    expect(parseErrorBody(null, 503).code).toBe('server');
  });
});
