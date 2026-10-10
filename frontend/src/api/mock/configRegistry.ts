// U4 mock: config registry + layered resolution (default < file < env < override).
// Mirrors backend/internal/service/config_service.go so the Settings UI is testable in mock mode.
import type {
  ConfigKeyView,
  ConfigSection,
  ConfigMeta,
  ConfigEnvVar,
  ConfigKeyType,
  ListConfigResponse,
} from '../../types/api';
import { ApiClientError } from '../errors';

interface RegKey {
  key: string;
  section: string;
  type: ConfigKeyType;
  description: string;
  default_value: unknown;
  env_var?: string;
  default_is_derived?: boolean;
  requires_restart: boolean;
  editable: boolean;
  json_object?: boolean; // P2.14: JSON-объект (текст-поле)
}

const SECTION_DESC: Record<string, string> = {
  server: 'HTTP-сервер',
  storage: 'База данных',
  sessions: 'Сессии агентов',
  watchdog: 'Watchdog-мониторинг',
  security: 'Безопасность',
  logging: 'Логирование',
  cost: 'Стоимость LLM-сессий (P2.14)',
  workspace: 'Рабочая область (workspace)',
};

const SECTION_ORDER = ['server', 'storage', 'tasks', 'sessions', 'watchdog', 'security', 'logging', 'cost', 'workspace'];

export const CONFIG_REGISTRY: RegKey[] = [
  { key: 'server.listen_addr', section: 'server', type: 'string', description: 'Адрес прослушивания HTTP', default_value: ':8080', env_var: 'DAEMON_LISTEN_ADDR', requires_restart: true, editable: true },
  { key: 'server.read_timeout', section: 'server', type: 'duration', description: 'Read-таймаут HTTP (константа)', default_value: '15s', requires_restart: true, editable: false },
  { key: 'server.write_timeout', section: 'server', type: 'duration', description: 'Write-таймаут HTTP (константа)', default_value: '30s', requires_restart: true, editable: false },
  { key: 'server.shutdown_timeout', section: 'server', type: 'duration', description: 'Таймаут graceful shutdown (константа)', default_value: '10s', requires_restart: true, editable: false },
  { key: 'storage.dsn', section: 'storage', type: 'string', description: 'DSN БД (sqlite:<path> или postgres://...)', default_value: 'sqlite:./daemon.db', env_var: 'DAEMON_DB_DSN', requires_restart: true, editable: true },
  { key: 'storage.migrate', section: 'storage', type: 'boolean', description: 'Мигрировать при старте', default_value: true, env_var: 'DAEMON_MIGRATE', requires_restart: true, editable: true },
  { key: 'tasks.closure_required_secs', section: 'tasks', type: 'duration', description: 'Дедлайн закрытия: сколько у задачи на закрытие после in_progress (read-at-transition, без рестарта)', default_value: '7200s', env_var: 'DAEMON_TASKS_CLOSURE_REQUIRED_SECS', requires_restart: false, editable: true },
  { key: 'sessions.poll_interval', section: 'sessions', type: 'duration', description: 'Период reaper-проверки сессий', default_value: '5s', env_var: 'DAEMON_SESSION_POLL_SECS', requires_restart: true, editable: true },
  { key: 'sessions.configs_dir', section: 'sessions', type: 'path', description: 'Конфиги pi-сессий', default_value: 'configs/sessions', env_var: 'DAEMON_SESSION_CONFIGS_DIR', requires_restart: true, editable: true },
  { key: 'watchdog.enabled', section: 'watchdog', type: 'boolean', description: 'Watchdog включён', default_value: true, env_var: 'DAEMON_WATCHDOG_ENABLED', requires_restart: true, editable: true },
  { key: 'watchdog.scan_interval', section: 'watchdog', type: 'duration', description: 'Интервал сканирования', default_value: '30s', env_var: 'DAEMON_WATCHDOG_SCAN_SECS', requires_restart: true, editable: true },
  { key: 'watchdog.stale_after', section: 'watchdog', type: 'duration', description: 'in_progress без обновлений → stale', default_value: '2h', env_var: 'DAEMON_WATCHDOG_STALE_SECS', requires_restart: true, editable: true },
  { key: 'watchdog.blocked_after', section: 'watchdog', type: 'duration', description: 'blocked дольше → алерт', default_value: '1h', env_var: 'DAEMON_WATCHDOG_BLOCKED_SECS', requires_restart: true, editable: true },
  { key: 'security.api_keys', section: 'security', type: 'string-list', description: 'API-ключи (readonly)', default_value: [], env_var: 'DAEMON_API_KEYS', requires_restart: true, editable: false },
  { key: 'security.secret_key', section: 'security', type: 'string', description: 'AES-256-GCM ключ (secret, readonly)', default_value: '', env_var: 'DAEMON_SECRET_KEY', requires_restart: true, editable: false },
  { key: 'security.agent_specs_dir', section: 'security', type: 'path', description: 'Каталог agent-spec', default_value: 'agents', env_var: 'DAEMON_AGENT_SPECS_DIR', requires_restart: true, editable: true },
  { key: 'logging.level', section: 'logging', type: 'string', description: 'Уровень логов (debug|info|warn|error)', default_value: 'info', env_var: 'DAEMON_LOG_LEVEL', requires_restart: true, editable: true },
  { key: 'cost.model_prices', section: 'cost', type: 'string', description: 'Цены моделей, JSON: {"<model>":{"in_usd_per_mtok":N,"out_usd_per_mtok":N}}. Пусто = «нет данных».', default_value: '{}', env_var: 'DAEMON_COST_MODEL_PRICES', requires_restart: false, editable: true, json_object: true },
  { key: 'cost.budget_alert_cents', section: 'cost', type: 'integer', description: 'Порог бюджета в ЦЕНТАХ (0 = выключен). Стоимость всех сессий > порога → watchdog-алерт (dedup).', default_value: 0, env_var: 'DAEMON_COST_BUDGET_ALERT_CENTS', requires_restart: false, editable: true },
  { key: 'workspace.root', section: 'workspace', type: 'path', description: 'Корень workspace (дефолт — cwd)', default_value: '.', env_var: 'DAEMON_WORKSPACE_ROOT', default_is_derived: true, requires_restart: true, editable: true },
  { key: 'workspace.artifacts_dir', section: 'workspace', type: 'path', description: 'Папка артефактов (дефолт — root/artifacts)', default_value: 'artifacts', env_var: 'DAEMON_WORKSPACE_ARTIFACTS', default_is_derived: true, requires_restart: true, editable: true },
  { key: 'workspace.screenshots_dir', section: 'workspace', type: 'path', description: 'Папка скриншотов (дефолт — root/screenshots)', default_value: 'screenshots', env_var: 'DAEMON_WORKSPACE_SCREENSHOTS', default_is_derived: true, requires_restart: true, editable: true },
  { key: 'workspace.logs_dir', section: 'workspace', type: 'path', description: 'Transcript-логи сессий', default_value: 'logs/sessions', env_var: 'DAEMON_LOGS_DIR', default_is_derived: true, requires_restart: true, editable: true },
];

// Simulated env + file layers (so all four sources are demonstrable in mock).
const MOCK_ENV: Record<string, string> = {
  DAEMON_LISTEN_ADDR: '127.0.0.1:8080',
  DAEMON_LOG_LEVEL: 'debug',
};
const MOCK_FILE: Record<string, string> = {
  DAEMON_DB_DSN: 'sqlite:./mock.db',
};

function parseValue(type: ConfigKeyType, raw: string): unknown {
  if (type === 'boolean') return raw === 'true';
  if (type === 'integer') return Number(raw);
  if (type === 'string-list') return raw ? raw.split(',').map((s) => s.trim()).filter(Boolean) : [];
  return raw;
}

const SECRET_KEYS = new Set(['security.secret_key', 'security.api_keys']);

function mkView(k: RegKey, currentValue: unknown, source: ConfigKeyView['source'], sourceDetail: string): ConfigKeyView {
  return {
    key: k.key,
    section: k.section,
    type: k.type,
    description: k.description,
    current_value: currentValue,
    default_value: k.default_value,
    source,
    source_detail: sourceDetail || undefined,
    default_is_derived: k.default_is_derived ?? false,
    requires_restart: k.requires_restart,
    editable: k.editable,
    json_object: k.json_object ?? undefined,
  };
}

function resolveKey(k: RegKey, overrides: Record<string, unknown>): ConfigKeyView {
  if (k.key in overrides) return mkView(k, overrides[k.key], 'override', '');
  if (k.env_var && MOCK_ENV[k.env_var]) return mkView(k, parseValue(k.type, MOCK_ENV[k.env_var]), 'env', k.env_var);
  if (k.env_var && MOCK_FILE[k.env_var] !== undefined) return mkView(k, parseValue(k.type, MOCK_FILE[k.env_var]), 'file', 'daemon.yaml');
  return mkView(k, k.default_value, 'default', '');
}

export function resolveConfig(overrides: Record<string, unknown>): ListConfigResponse {
  const sections: ConfigSection[] = SECTION_ORDER.map((sec) => ({
    name: sec,
    description: SECTION_DESC[sec],
    keys: CONFIG_REGISTRY.filter((k) => k.section === sec).map((k) => resolveKey(k, overrides)),
  }));
  const byKey = new Map<string, ConfigKeyView>();
  for (const s of sections) for (const k of s.keys) byKey.set(k.key, k);

  const environment: ConfigEnvVar[] = CONFIG_REGISTRY.filter((k) => k.env_var)
    .map((k) => {
      const view = byKey.get(k.key)!;
      const setInEnv = !!(k.env_var && MOCK_ENV[k.env_var]);
      let value = setInEnv ? String(MOCK_ENV[k.env_var!]) : '';
      if (SECRET_KEYS.has(k.key) && value) value = '•••';
      return { env_var: k.env_var!, value, feeds_key: k.key, source_active: view.source === 'env' };
    });

  const meta: ConfigMeta = {
    project: 'team',
    instance: 'local',
    version: '1.0.0',
    uptime_seconds: 3600,
    git_commit: 'mock000',
    git_branch: 'master',
    config_hash: 'sha256:mock-config',
    started_at: new Date(Date.now() - 3600_000).toISOString(),
  };
  return { meta, sections, environment };
}

// Normalize + type-validate a set value (mirrors backend → 400 on bad type).
export function normalizeSetValue(type: ConfigKeyType, value: unknown): unknown {
  switch (type) {
    case 'boolean':
      if (typeof value === 'boolean') return value;
      if (value === 'true') return true;
      if (value === 'false') return false;
      throw new ApiClientError(400, 'validation_failed', `expected boolean for this key, got ${JSON.stringify(value)}`);
    case 'integer': {
      const n = typeof value === 'number' ? value : Number(value);
      if (!Number.isFinite(n)) throw new ApiClientError(400, 'validation_failed', 'expected integer');
      return Math.trunc(n);
    }
    case 'string-list':
      if (Array.isArray(value)) return value.map(String);
      if (typeof value === 'string') return value.split(',').map((s) => s.trim()).filter(Boolean);
      throw new ApiClientError(400, 'validation_failed', 'expected string-list');
    default: // string | path | duration
      if (typeof value !== 'string') throw new ApiClientError(400, 'validation_failed', 'expected string');
      return value;
  }
}
