// Минимальные ambient-объявления node:* — в проекте нет @types/node (и новых
// зависимостей нельзя, по правилам задачи). Используется только в тестах
// (runtime vitest = Node, эти модули доступны), чтобы читать topology.css как текст.
declare module 'node:fs' {
  export function readFileSync(path: string, encoding: string): string;
  export function readdirSync(path: string): string[];
  export function statSync(path: string): { isDirectory(): boolean };
}
declare module 'node:url' {
  export function fileURLToPath(url: string | URL): string;
}
declare module 'node:path' {
  export function resolve(...parts: string[]): string;
  export function dirname(p: string): string;
  export function join(...parts: string[]): string;
  export function relative(from: string, to: string): string;
}
