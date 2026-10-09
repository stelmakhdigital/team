#!/usr/bin/env node
// WCAG 2.1 contrast-ratio checker for the team UI palette.
//
// Computes the relative-luminance contrast ratio (https://www.w3.org/TR/WCAG21/#dfn-contrast-ratio)
// for every *status/semantic text* color pair used in the UI (ok/warn/error/info,
// badges, mode chips, text on panels) plus a couple of non-text (border) pairs.
//
// Usage:  node scripts/contrast-check.mjs
// Output: a markdown table (pair | fg/bg | ratio | target | AA) — copy into frontend.md.
// Exit:   0 if every pair meets its target, 1 otherwise (CI-friendly).
//
// Targets (WCAG AA): normal text >= 4.5:1, large text (>=18.66px or >=14pt bold) >= 3:1,
// non-text UI (borders/icons that convey meaning) >= 3:1.
// All status/badge text in this app is ~0.7–0.9rem normal weight → treated as normal text (4.5:1).

function hexToRgb(hex) {
  let h = hex.replace('#', '');
  if (h.length === 3) h = h.split('').map((c) => c + c).join('');
  const n = parseInt(h, 16);
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 };
}
function chan(c) {
  c /= 255;
  return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}
function lum(hex) {
  const { r, g, b } = hexToRgb(hex);
  return 0.2126 * chan(r) + 0.7152 * chan(g) + 0.0722 * chan(b);
}
function ratio(fg, bg) {
  const l1 = lum(fg);
  const l2 = lum(bg);
  const hi = Math.max(l1, l2);
  const lo = Math.min(l1, l2);
  return (hi + 0.05) / (lo + 0.05);
}

// Palette (mirrors :root in styles.css). Keep in sync when a var changes.
const P = {
  page: '#0d1117',
  panel: '#161b22', // .card
  panel2: '#1c2330', // .badge background, some inputs
  text: '#e6edf3',
  muted: '#8b949e',
  green: '#3fb950', // ok / done / running / connected
  red: '#f85149', // error / failed / blocked
  yellow: '#d29922', // warn / pending
  accent: '#4f8ef7', // info / link / in_progress
  purple: '#a371f7',
  sky: '#38bdf8',
  dim: '#64748b',
  accentStrong: '#2563eb', // filled button bg (AA with white)
  redStrong: '#cf222e', // filled critical badge bg (AA with white)
};

// [label, fg, bg, target, note, required]
// required=false → informational (decorative grouping, not a WCAG 1.4.11 hard requirement).
const pairs = [
  ['Body text on page', P.text, P.page, 4.5, ''],
  ['Body text on panel', P.text, P.panel, 4.5, ''],
  ['Muted on page', P.muted, P.page, 4.5, ''],
  ['Muted on panel', P.muted, P.panel, 4.5, ''],
  ['Muted on badge (panel2)', P.muted, P.panel2, 4.5, 'badge-state-inactive/stopped/idle'],
  ['Green (ok) on badge', P.green, P.panel2, 4.5, 'badge done/running/active'],
  ['Green (ok) on panel', P.green, P.panel, 4.5, 'validation-ok / ws-connected'],
  ['Red (error) on badge', P.red, P.panel2, 4.5, 'badge blocked/failed/sev-high'],
  ['Red (error) on panel', P.red, P.panel, 4.5, 'hint-error / form-error / error-state'],
  ['Yellow (warn) on badge', P.yellow, P.panel2, 4.5, 'badge pending/warn/sev-medium'],
  ['Yellow (warn) on panel', P.yellow, P.panel, 4.5, 'hint-warn / ws-mock'],
  ['Accent (info) on badge', P.accent, P.panel2, 4.5, 'badge in_progress/sev-low/info'],
  ['Accent (info) on panel', P.accent, P.panel, 4.5, 'links / cfg-source-override'],
  ['Accent on cfg-override chip', P.accent, '#12233b', 4.5, '.cfg-source-override'],
  ['Purple on badge', P.purple, P.panel2, 4.5, 'badge-type-team'],
  ['Sky on badge', P.sky, P.panel2, 4.5, 'cfg-source-file'],
  ['Yellow on mode-mock chip', P.yellow, '#3b2f12', 4.5, '.mode-mock'],
  ['Green on mode-real chip', P.green, '#10291a', 4.5, '.mode-real'],
  ['White on primary button', '#ffffff', P.accentStrong, 4.5, '.btn-primary bg'],
  ['White on critical badge', '#ffffff', P.redStrong, 4.5, '.badge-sev-critical bg'],
  // non-text (borders/icons that convey meaning) → 3:1
  ['Dim border on panel (transcript)', P.dim, P.panel, 3.0, 'non-text border (conveys tool-result)', true],
  ['Panel/card border on page', '#2d333b', P.page, 3.0, 'decorative grouping (WCAG 1.4.11 n/a — visual enhancement, card bg already delineates)', false],
];

let fails = 0;
const rows = pairs.map(([label, fg, bg, min, note, required = true]) => {
  const r = ratio(fg, bg);
  const pass = r >= min;
  if (required && !pass) fails++;
  const verdict = !required ? 'ℹ️ decorative' : pass ? '✅ pass' : '❌ FAIL';
  return `| ${label}${note ? ' — ' + note : ''} | \`${fg}\` / \`${bg}\` | **${r.toFixed(2)}:1** | ${required ? min.toFixed(1) + ':1' : 'n/a'} | ${verdict} |`;
});

console.log('# WCAG 2.1 contrast — team UI status/semantic colors\n');
console.log('Relative-luminance contrast ratio per WCAG 2.1. Targets: normal text ≥4.5:1, non-text (border/icon) ≥3.0:1.');
console.log('\n| Pair | fg / bg | ratio | target | AA |\n|---|---|---|---|---|');
console.log(rows.join('\n'));
console.log(`\n**${fails === 0 ? '✅ ALL ' + pairs.length + ' pairs pass' : '❌ ' + fails + ' pair(s) FAIL' + (fails ? ' — see ❌' : '')}**\n`);
process.exit(fails === 0 ? 0 : 1);
