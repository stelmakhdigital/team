import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api } from '../api';
import { fuzzyRank, highlightParts, type FuzzyResult } from '../lib/fuzzy';

// P2.10 — Command palette (Cmd+K / Ctrl+K): fuzzy-переход по страницам + сущностям
// + быстрым действиям. Собственный компонент (без UI-библиотек); fuzzy — lib/fuzzy.ts.

interface PageItem {
  label: string;
  path: string;
  desc: string;
  icon: string;
}

interface ActionItem {
  label: string;
  icon: string;
  run: () => void;
}

interface EntityItem {
  kind: string;
  id: number;
  name: string;
  subtitle: string;
  path: string;
  icon: string;
}

type Section = 'pages' | 'actions' | 'entities';

interface FlatItem {
  section: Section;
  key: string;
  label: string;
  icon: string;
  subtitle?: string;
  match: FuzzyResult;
  path?: string;
  action?: () => void;
}

const PAGES: PageItem[] = [
  { label: 'Dashboard', path: '/', desc: 'Обзор команды и задач', icon: '▦' },
  { label: 'Attention', path: '/attention', desc: 'Что требует меня', icon: '⚑' },
  { label: 'Pulse', path: '/pulse', desc: 'Единая лента событий', icon: '📡' },
  { label: 'Teams', path: '/teams', desc: 'Команды и топологии', icon: '⬡' },
  { label: 'Projects', path: '/projects', desc: 'Проекты', icon: '▣' },
  { label: 'Tasks', path: '/tasks', desc: 'Задачи', icon: '☰' },
  { label: 'Workflows', path: '/workflows', desc: 'Воркфлоу', icon: '⑃' },
  { label: 'Watchdog', path: '/watchdog/jobs', desc: 'Watchdog-джобы', icon: '⏰' },
  { label: 'Messages', path: '/messages', desc: 'Сообщения', icon: '✉' },
  { label: 'Library', path: '/library', desc: 'Библиотека', icon: '▤' },
  { label: 'History', path: '/history', desc: 'История и аудит', icon: '⏱' },
  { label: 'Transcript search', path: '/transcript-search', desc: 'Поиск по транскриптам', icon: '🔎' },
  { label: 'Diagnostics', path: '/diagnostics', desc: 'Диагностика / health', icon: '🩺' },
  { label: 'Settings', path: '/settings', desc: 'Настройки', icon: '⚙' },
];

const KIND_ICON: Record<string, string> = {
  task: '☰',
  session: '◉',
  workflow: '⑃',
  project: '▣',
  team: '⬡',
};
const KIND_LABEL: Record<string, string> = {
  task: 'Задача',
  session: 'Сессия',
  workflow: 'Воркфлоу',
  project: 'Проект',
  team: 'Команда',
};

const DEBOUNCE_MS = 200;
const MAX_PER_SECTION = 8;

function Highlight({ text, indices }: { text: string; indices: number[] }) {
  if (indices.length === 0) return <>{text}</>;
  const parts = highlightParts(text, indices);
  return (
    <>
      {parts.map((p, i) =>
        p.hit ? (
          <span key={i} className="palette-hl">
            {p.char}
          </span>
        ) : (
          <span key={i}>{p.char}</span>
        ),
      )}
    </>
  );
}

export default function CommandPalette() {
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [activeIndex, setActiveIndex] = useState(0);
  const [entities, setEntities] = useState<EntityItem[]>([]);
  const [entityLoading, setEntityLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const fetchToken = useRef(0);

  const actions = useMemo<ActionItem[]>(
    () => [
      { label: 'New team', icon: '＋', run: () => navigate('/teams') },
      { label: 'New project', icon: '＋', run: () => navigate('/projects') },
      { label: 'New task', icon: '＋', run: () => navigate('/tasks') },
      { label: 'New workflow', icon: '＋', run: () => navigate('/workflows') },
      { label: 'Run diagnostics', icon: '🩺', run: () => navigate('/diagnostics') },
      { label: 'Go to Attention', icon: '⚑', run: () => navigate('/attention') },
    ],
    [navigate],
  );

  // Открытие: Cmd+K / Ctrl+K (глобально)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setOpen(true);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  // Autofocus input при открытии + сброс
  useEffect(() => {
    if (open) {
      setQuery('');
      setActiveIndex(0);
      setEntities([]);
      const id = requestAnimationFrame(() => inputRef.current?.focus());
      return () => cancelAnimationFrame(id);
    }
  }, [open]);

  // Lazy entity-поиск: по query >0 символов, debounce, параллельный fetch списков.
  useEffect(() => {
    if (!open) return;
    if (query.trim().length < 1) {
      setEntities([]);
      return;
    }
    setEntityLoading(true);
    const token = ++fetchToken.current;
    const t = setTimeout(async () => {
      const [tasks, sessions, workflows, projects, teams] = await Promise.allSettled([
        api.tasks.list({ limit: 50 }),
        api.sessions.list(),
        api.workflows.getWorkflows(),
        api.projects.list({ limit: 50 }),
        api.teams.getTeams(),
      ]);
      if (token !== fetchToken.current) return; // устаревший результат
      const out: EntityItem[] = [];
      if (tasks.status === 'fulfilled')
        for (const t of tasks.value.tasks)
          out.push({ kind: 'task', id: t.id, name: t.title, subtitle: `${t.state} · ${t.team_name}`, path: `/tasks/${t.id}`, icon: KIND_ICON.task });
      if (sessions.status === 'fulfilled')
        for (const s of sessions.value.sessions)
          out.push({ kind: 'session', id: s.id, name: `Session #${s.id} ${s.role_name ? `· ${s.role_name}` : ''}`, subtitle: `${s.state} · ${s.team_name}`, path: `/sessions/${s.id}`, icon: KIND_ICON.session });
      if (workflows.status === 'fulfilled')
        for (const w of workflows.value.workflows)
          out.push({ kind: 'workflow', id: w.id, name: w.name, subtitle: KIND_LABEL.workflow, path: `/workflows/${w.id}`, icon: KIND_ICON.workflow });
      if (projects.status === 'fulfilled')
        for (const p of projects.value.projects)
          out.push({ kind: 'project', id: p.id, name: p.name, subtitle: KIND_LABEL.project, path: `/projects/${p.id}`, icon: KIND_ICON.project });
      if (teams.status === 'fulfilled')
        for (const tm of teams.value.teams)
          out.push({ kind: 'team', id: tm.id, name: tm.name, subtitle: KIND_LABEL.team, path: `/teams/${tm.id}`, icon: KIND_ICON.team });
      setEntities(out);
      setEntityLoading(false);
    }, DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [open, query]);

  // Плоский список результатов (порядок разделов: pages → actions → entities).
  const sections = useMemo(() => {
    const q = query;
    const pages = fuzzyRank(PAGES, q, (p) => `${p.label} ${p.desc}`).slice(0, MAX_PER_SECTION);
    const acts = fuzzyRank(actions, q, (a) => a.label).slice(0, MAX_PER_SECTION);
    const ents = q.trim() ? fuzzyRank(entities, q, (e) => `${e.name} ${e.subtitle}`).slice(0, MAX_PER_SECTION) : [];
    const flat: FlatItem[] = [
      ...pages.map((x) => ({
        section: 'pages' as Section,
        key: `page-${x.item.path}`,
        label: x.item.label,
        icon: x.item.icon,
        subtitle: x.item.desc,
        match: x.match,
        path: x.item.path,
      })),
      ...acts.map((x, i) => ({
        section: 'actions' as Section,
        key: `action-${i}`,
        label: x.item.label,
        icon: x.item.icon,
        subtitle: 'Действие',
        match: x.match,
        action: x.item.run,
      })),
      ...ents.map((x) => ({
        section: 'entities' as Section,
        key: `entity-${x.item.kind}-${x.item.id}`,
        label: x.item.name,
        icon: x.item.icon,
        subtitle: `${KIND_LABEL[x.item.kind]} · ${x.item.subtitle}`,
        match: x.match,
        path: x.item.path,
      })),
    ];
    return { pages, actions: acts, entities: ents, flat };
  }, [query, entities, actions]);

  // activeIndex — в пределах flat-списка
  useEffect(() => {
    if (activeIndex >= sections.flat.length) setActiveIndex(Math.max(0, sections.flat.length - 1));
  }, [sections.flat.length, activeIndex]);

  const close = useCallback(() => setOpen(false), []);
  const runItem = useCallback(
    (item: FlatItem | undefined) => {
      if (!item) return;
      close();
      if (item.path) navigate(item.path);
      else item.action?.();
    },
    [close, navigate],
  );

  const onInputKey = useCallback(
    (e: React.KeyboardEvent) => {
      const n = sections.flat.length;
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        if (n) setActiveIndex((i) => (i + 1) % n);
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        if (n) setActiveIndex((i) => (i - 1 + n) % n);
      } else if (e.key === 'Enter') {
        e.preventDefault();
        runItem(sections.flat[activeIndex]);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        close();
      } else if (e.key === 'Tab') {
        e.preventDefault(); // focus-trap: не уходим из палитры
      }
    },
    [sections.flat, activeIndex, runItem, close],
  );

  if (!open) return null;

  const renderSection = (
    title: string,
    items: FlatItem[],
    offset: number,
    empty?: boolean,
  ) => {
    if (empty) return null;
    if (items.length === 0) return null;
    return (
      <li role="presentation" className="palette-section">
        <div className="palette-section-head">{title}</div>
        <ul role="presentation">
          {items.map((it, i) => {
            const idx = offset + i;
            return (
              <li
                key={it.key}
                id={`palette-opt-${idx}`}
                role="option"
                aria-selected={idx === activeIndex}
                className={`palette-item${idx === activeIndex ? ' active' : ''}`}
                onMouseEnter={() => setActiveIndex(idx)}
                onClick={() => runItem(it)}
              >
                <span className="palette-icon" aria-hidden="true">
                  {it.icon}
                </span>
                <span className="palette-item-main">
                  <span className="palette-item-label">
                    <Highlight text={it.label} indices={it.match.indices} />
                  </span>
                  {it.subtitle ? <span className="palette-item-sub muted">{it.subtitle}</span> : null}
                </span>
              </li>
            );
          })}
        </ul>
      </li>
    );
  };

  const pagesStart = 0;
  const actionsStart = sections.pages.length;
  const entitiesStart = sections.pages.length + sections.actions.length;

  return (
    <div className="palette-overlay" onMouseDown={close} aria-hidden={false}>
      <div
        className="palette-modal"
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        onMouseDown={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          className="palette-input"
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-listbox"
          aria-activedescendant={sections.flat[activeIndex] ? `palette-opt-${activeIndex}` : undefined}
          aria-label="Command palette"
          placeholder="Поиск: страницы, сущности, действия…  (Esc — закрыть)"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setActiveIndex(0);
          }}
          onKeyDown={onInputKey}
        />
        <ul id="palette-listbox" role="listbox" className="palette-list" aria-label="Результаты">
          {sections.flat.length === 0 ? (
            <li className="palette-empty muted">
              Ничего не найдено{entityLoading ? ' (поиск сущностей…)' : ''}
            </li>
          ) : (
            <>
              {renderSection('Страницы', sections.flat.slice(pagesStart, actionsStart), pagesStart)}
              {renderSection('Действия', sections.flat.slice(actionsStart, entitiesStart), actionsStart)}
              {renderSection('Сущности', sections.flat.slice(entitiesStart), entitiesStart, query.trim().length < 1)}
            </>
          )}
        </ul>
        <div className="palette-footer muted">
          <span>↑/↓ — выбор · Enter — перейти · Esc — закрыть</span>
          {entityLoading ? <span className="palette-loading">поиск…</span> : null}
        </div>
      </div>
    </div>
  );
}
