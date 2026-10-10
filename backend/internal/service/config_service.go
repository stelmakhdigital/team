package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"daemon/internal/config"
	"daemon/internal/repository"
)

// ConfigKeyType — типизация ключа конфигурации (U4, config-contract).
type ConfigKeyType string

const (
	StrType     ConfigKeyType = "string"
	IntType     ConfigKeyType = "integer"
	DurType     ConfigKeyType = "duration"
	BoolType    ConfigKeyType = "boolean"
	PathType    ConfigKeyType = "path"
	StrListType ConfigKeyType = "string-list"
)

// Sources разрешения: default < file < env < override.
const (
	SourceDefault  = "default"
	SourceFile     = "file"
	SourceEnv      = "env"
	SourceOverride = "override"
)

// KeySpec — запись реестра ключей демона (полный охват, config-contract).
type KeySpec struct {
	Key              string
	Section          string
	Type             ConfigKeyType
	Description      string
	Default          string // каноническая строка дефолта
	DefaultIsDerived bool   // дефолт host-derived (cwd), не константа
	DeriveFromKey    string // "" = нет; иначе родительский ключ (derivation)
	DeriveSuffix     string // суффикс (подпапка) при DeriveFromKey
	RequiresRestart  bool
	Editable         bool
	Secret           bool
	EnvVar           string // "" = без env-слоя (константа)
}

// configSections — порядок и описания секций (config-contract).
var configSections = []struct{ name, description string }{
	{"server", "HTTP-сервер (адрес, таймауты)"},
	{"storage", "Хранилище (DB DSN, миграции)"},
	{"tasks", "Задачи (дедлайн закрытия)"},
	{"sessions", "Сессии и runtime (reaper, конфиги pi-сессий)"},
	{"watchdog", "Watchdog живости сессий (stale/blocked)"},
	{"security", "Безопасность (API-ключи, secret key, specs)"},
	{"logging", "Логи демона (уровень)"},
	{"workspace", "Рабочая область (root, артефакты, скриншоты, логи)"},
}

// configRegistry — реестр ВСЕХ ключей демона (config-contract: полный охват).
// Дефолты должны совпадать с internal/config/config.go (Load).
func configRegistry() []KeySpec {
	return []KeySpec{
		{Key: "server.listen_addr", Section: "server", Type: StrType,
			Description: "Адрес прослушивания HTTP", Default: ":8080",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_LISTEN_ADDR"},
		{Key: "server.read_timeout", Section: "server", Type: DurType,
			Description: "Read-таймаут HTTP-сервера (константа)", Default: "15s", RequiresRestart: true},
		{Key: "server.write_timeout", Section: "server", Type: DurType,
			Description: "Write-таймаут HTTP-сервера (константа)", Default: "30s", RequiresRestart: true},
		{Key: "server.shutdown_timeout", Section: "server", Type: DurType,
			Description: "Таймаут graceful shutdown (константа)", Default: "10s", RequiresRestart: true},
		{Key: "storage.dsn", Section: "storage", Type: StrType,
			Description: "DSN базы данных (sqlite:<path> или postgres://...)", Default: "sqlite:./daemon.db",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_DB_DSN"},
		{Key: "storage.migrate", Section: "storage", Type: BoolType,
			Description: "Применять миграции на старте", Default: "true",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_MIGRATE"},
		{Key: "tasks.closure_required_secs", Section: "tasks", Type: DurType,
			Description: "Дедлайн закрытия задачи: при переходе в in_progress = now + значение (пересчитывается при повторном входе); читается на момент перехода",
			Default:     "7200s", RequiresRestart: false, Editable: true, EnvVar: "DAEMON_TASKS_CLOSURE_REQUIRED_SECS"},
		{Key: "sessions.poll_interval", Section: "sessions", Type: DurType,
			Description: "Интервал reaper-проверки живости сессий", Default: "5s",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_SESSION_POLL_SECS"},
		{Key: "sessions.configs_dir", Section: "sessions", Type: PathType,
			Description: "Каталог конфигов pi-сессий", Default: "configs/sessions",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_SESSION_CONFIGS_DIR"},
		{Key: "sessions.context_wall_pct", Section: "sessions", Type: IntType,
			Description: "Порог context_used_percentage (%), при котором сессия попадает в compact-plan (P1.5); читается в рантайме",
			Default:     "80", RequiresRestart: false, Editable: true, EnvVar: "DAEMON_SESSION_CONTEXT_WALL_PCT"},
		{Key: "watchdog.enabled", Section: "watchdog", Type: BoolType,
			Description: "Включён ли watchdog (stale/blocked)", Default: "true",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_WATCHDOG_ENABLED"},
		{Key: "watchdog.scan_interval", Section: "watchdog", Type: DurType,
			Description: "Интервал сканирования watchdog", Default: "30s",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_WATCHDOG_SCAN_SECS"},
		{Key: "watchdog.stale_after", Section: "watchdog", Type: DurType,
			Description: "in_progress без обновлений → stale", Default: "2h",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_WATCHDOG_STALE_SECS"},
		{Key: "watchdog.blocked_after", Section: "watchdog", Type: DurType,
			Description: "blocked дольше → алерт", Default: "1h",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_WATCHDOG_BLOCKED_SECS"},
		{Key: "security.api_keys", Section: "security", Type: StrListType,
			Description: "API-ключи из env (admin; readonly — ключи из UI в DB)", Default: "",
			RequiresRestart: true, Secret: true, EnvVar: "DAEMON_API_KEYS"},
		{Key: "security.secret_key", Section: "security", Type: StrType,
			Description: "AES-256-GCM ключ для secrets (64 hex; secret, readonly)", Default: "",
			RequiresRestart: true, Secret: true, EnvVar: "DAEMON_SECRET_KEY"},
		{Key: "security.agent_specs_dir", Section: "security", Type: PathType,
			Description: "Каталог agent-spec'ов", Default: "agents",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_AGENT_SPECS_DIR"},
		{Key: "logging.level", Section: "logging", Type: StrType,
			Description: "Уровень логов (debug|info|warn|error)", Default: "info",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_LOG_LEVEL"},
		{Key: "workspace.root", Section: "workspace", Type: PathType,
			Description: "Корень workspace (дефолт — cwd)", Default: ".",
			DefaultIsDerived: true, RequiresRestart: true, Editable: true, EnvVar: "DAEMON_WORKSPACE_ROOT"},
		{Key: "workspace.artifacts_dir", Section: "workspace", Type: PathType,
			Description:      "Папка артефактов (дефолт — workspace.root/artifacts)",
			DefaultIsDerived: true, DeriveFromKey: "workspace.root", DeriveSuffix: "artifacts",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_WORKSPACE_ARTIFACTS"},
		{Key: "workspace.screenshots_dir", Section: "workspace", Type: PathType,
			Description:      "Папка скриншотов (дефолт — workspace.root/screenshots)",
			DefaultIsDerived: true, DeriveFromKey: "workspace.root", DeriveSuffix: "screenshots",
			RequiresRestart: true, Editable: true, EnvVar: "DAEMON_WORKSPACE_SCREENSHOTS"},
		{Key: "workspace.logs_dir", Section: "workspace", Type: PathType,
			Description: "Папка transcript-логов сессий (относительно cwd)", Default: "logs/sessions",
			DefaultIsDerived: true, RequiresRestart: true, Editable: true, EnvVar: "DAEMON_LOGS_DIR"},
	}
}

// ConfigMeta — метаданные инстанса (config-contract, секция Meta).
type ConfigMeta struct {
	Project       string `json:"project"`
	Instance      string `json:"instance"`
	Version       string `json:"version"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	GitCommit     string `json:"git_commit,omitempty"`
	GitBranch     string `json:"git_branch,omitempty"`
	ConfigHash    string `json:"config_hash"`
	StartedAt     string `json:"started_at"`
}

// ConfigKeyView — ключ конфигурации с решённым значением и провенансом.
type ConfigKeyView struct {
	Key              string `json:"key"`
	Section          string `json:"section"`
	Type             string `json:"type"`
	Description      string `json:"description"`
	CurrentValue     any    `json:"current_value"`
	DefaultValue     any    `json:"default_value"`
	Source           string `json:"source"`
	SourceDetail     string `json:"source_detail,omitempty"`
	DefaultIsDerived bool   `json:"default_is_derived"`
	RequiresRestart  bool   `json:"requires_restart"`
	Editable         bool   `json:"editable"`
}

// ConfigSection — секция с ключами.
type ConfigSection struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Keys        []ConfigKeyView `json:"keys"`
}

// EnvVarView — env-переменная демона, влияющая на конфиг (provenance).
type EnvVarView struct {
	EnvVar       string `json:"env_var"`
	Value        string `json:"value"`
	FeedsKey     string `json:"feeds_key"`
	SourceActive bool   `json:"source_active"`
}

// ListConfigResponse — ответ GET /api/v1/config.
type ListConfigResponse struct {
	Meta        ConfigMeta      `json:"meta"`
	Sections    []ConfigSection `json:"sections"`
	Environment []EnvVarView    `json:"environment"`
}

// SetOverrideResult — ответ PUT /api/v1/config/{key}.
type SetOverrideResult struct {
	Key             string `json:"key"`
	Value           any    `json:"value"`
	Source          string `json:"source"`
	RequiresRestart bool   `json:"requires_restart"`
	NeedsRestart    bool   `json:"needs_restart"`
}

// ResetOverrideResult — ответ DELETE /api/v1/config/{key}.
type ResetOverrideResult struct {
	Key    string `json:"key"`
	Source string `json:"source"`
	Value  any    `json:"value"`
}

// ConfigService — подсистема конфигурации (U4): реестр ключей, layered
// resolution (default < file < env < override), set/reset override, meta.
type ConfigService struct {
	db          *sql.DB
	repo        *repository.ConfigRepo
	registry    []KeySpec
	byKey       map[string]*KeySpec
	fileLayer   map[string]string
	filePath    string
	envSnapshot map[string]string // DAEMON_* на момент старта (демон грузит конфиг на старте)
	startedAt   time.Time
	gitCommit   string
	gitBranch   string
}

// NewConfigService — сборка: реестр + file-слой (DAEMON_CONFIG_FILE) + env-снапшот.
func NewConfigService(db *sql.DB) (*ConfigService, error) {
	filePath := config.FileLayerPath()
	fileLayer, err := config.LoadFileLayer(filePath)
	if err != nil {
		return nil, err
	}
	envSnapshot := map[string]string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "DAEMON_") {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) == 2 {
			envSnapshot[parts[0]] = parts[1]
		}
	}
	registry := configRegistry()
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	byKey := make(map[string]*KeySpec, len(registry))
	for i := range registry {
		if registry[i].Key == "workspace.root" {
			registry[i].Default = cwd // host-derived
		}
		byKey[registry[i].Key] = &registry[i]
	}
	commit, branch := gitInfo()
	return &ConfigService{
		db:          db,
		repo:        repository.NewConfigRepo(db),
		registry:    registry,
		byKey:       byKey,
		fileLayer:   fileLayer,
		filePath:    filePath,
		envSnapshot: envSnapshot,
		startedAt:   time.Now().UTC(),
		gitCommit:   commit,
		gitBranch:   branch,
	}, nil
}

// Registry — реестр ключей (для проверок/тестов).
func (s *ConfigService) Registry() []KeySpec {
	out := make([]KeySpec, len(s.registry))
	copy(out, s.registry)
	return out
}

// ResolveDuration — layered value (default < file < env < override) of a
// duration key, resolved at call time (P0.2: дедлайн закрытия читается на
// момент перехода, restart=false).
func (s *ConfigService) ResolveDuration(ctx context.Context, key string) (time.Duration, error) {
	spec, ok := s.byKey[key]
	if !ok {
		return 0, NewNotFound("config key " + key)
	}
	if spec.Type != DurType {
		return 0, NewValidation(fmt.Sprintf("config key %q is not a duration", key))
	}
	overrides, err := s.repo.All(ctx, s.db)
	if err != nil {
		return 0, NewInternal(err)
	}
	r, err := s.resolve(spec, overrides)
	if err != nil {
		return 0, err
	}
	d, ok := r.Value.(string)
	if !ok {
		return 0, NewInternal(fmt.Errorf("config key %q: unexpected duration value", key))
	}
	return time.ParseDuration(d)
}

// ResolveInt — layered value (default < file < env < override) as int (P1.5).
func (s *ConfigService) ResolveInt(ctx context.Context, key string) (int, error) {
	spec, ok := s.byKey[key]
	if !ok {
		return 0, NewNotFound("config key " + key)
	}
	if spec.Type != IntType {
		return 0, NewValidation(fmt.Sprintf("config key %q is not an integer", key))
	}
	overrides, err := s.repo.All(ctx, s.db)
	if err != nil {
		return 0, NewInternal(err)
	}
	r, err := s.resolve(spec, overrides)
	if err != nil {
		return 0, err
	}
	n, ok := r.Value.(int64)
	if !ok {
		return 0, NewInternal(fmt.Errorf("config key %q: unexpected integer value", key))
	}
	return int(n), nil
}

// ---------- Layered resolution: default < file < env < override ----------

// resolvedValue — решённое значение + провенанс.
type resolvedValue struct {
	Value  any    // типизированное (string/bool/int/[]string/duration-string)
	Raw    string // каноническая строка (для сравнения/переиспользования)
	Source string
	Detail string
}

func (s *ConfigService) resolve(spec *KeySpec, overrides map[string]string) (resolvedValue, error) {
	// override — наивысший приоритет
	if raw, ok := overrides[spec.Key]; ok {
		v, err := parseKeyValue(spec.Type, raw)
		if err != nil {
			return resolvedValue{}, err
		}
		return resolvedValue{Value: v, Raw: raw, Source: SourceOverride, Detail: "config_overrides"}, nil
	}
	// env
	if spec.EnvVar != "" {
		if raw, ok := s.envSnapshot[spec.EnvVar]; ok && raw != "" {
			v, err := parseKeyValue(spec.Type, raw)
			if err != nil {
				return resolvedValue{}, fmt.Errorf("invalid value for %s: %w", spec.Key, err)
			}
			return resolvedValue{Value: v, Raw: raw, Source: SourceEnv, Detail: spec.EnvVar}, nil
		}
	}
	// file
	if raw, ok := s.fileLayer[spec.Key]; ok {
		v, err := parseKeyValue(spec.Type, raw)
		if err != nil {
			return resolvedValue{}, fmt.Errorf("invalid value for %s in %s: %w", spec.Key, s.filePath, err)
		}
		return resolvedValue{Value: v, Raw: raw, Source: SourceFile, Detail: s.filePath}, nil
	}
	// default (+ derivation от resolved родителя)
	if spec.DeriveFromKey != "" {
		parent, ok := s.byKey[spec.DeriveFromKey]
		if !ok {
			return resolvedValue{}, fmt.Errorf("derivation: parent key %s not found", spec.DeriveFromKey)
		}
		pv, err := s.resolve(parent, overrides)
		if err != nil {
			return resolvedValue{}, err
		}
		if ps, ok := pv.Value.(string); ok {
			joined := filepath.Join(ps, spec.DeriveSuffix)
			return resolvedValue{Value: joined, Raw: joined, Source: SourceDefault}, nil
		}
	}
	v, err := parseKeyValue(spec.Type, spec.Default)
	if err != nil {
		return resolvedValue{}, fmt.Errorf("bad default for %s: %w", spec.Key, err)
	}
	return resolvedValue{Value: v, Raw: spec.Default, Source: SourceDefault}, nil
}

// defaultOf — значение из слоя default (для default_value; derivation от default родителя).
func (s *ConfigService) defaultOf(spec *KeySpec) (any, error) {
	if spec.DeriveFromKey != "" {
		parent, ok := s.byKey[spec.DeriveFromKey]
		if !ok {
			return nil, fmt.Errorf("derivation: parent key %s not found", spec.DeriveFromKey)
		}
		pv, err := s.defaultOf(parent)
		if err != nil {
			return nil, err
		}
		if ps, ok := pv.(string); ok {
			return filepath.Join(ps, spec.DeriveSuffix), nil
		}
	}
	return parseKeyValue(spec.Type, spec.Default)
}

// parseKeyValue — каноническая строка → типизированное значение по типу ключа.
func parseKeyValue(t ConfigKeyType, raw string) (any, error) {
	switch t {
	case StrType, PathType:
		return raw, nil
	case BoolType:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("expected boolean, got %q", raw)
		}
		return b, nil
	case IntType:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected integer, got %q", raw)
		}
		return n, nil
	case DurType:
		if d, err := time.ParseDuration(raw); err == nil {
			return d.String(), nil
		}
		// конвенция daemon: env-длительности — в секундах
		if d, err := time.ParseDuration(raw + "s"); err == nil {
			return d.String(), nil
		}
		return nil, fmt.Errorf("expected duration, got %q", raw)
	case StrListType:
		out := []string{}
		if raw != "" {
			for _, p := range strings.Split(raw, ",") {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown key type %q", t)
	}
}

// ---------- API ----------

// ListConfig — все ключи по секциям + meta + environment (GET /api/v1/config).
func (s *ConfigService) ListConfig(ctx context.Context) (*ListConfigResponse, error) {
	overrides, err := s.repo.All(ctx, s.db)
	if err != nil {
		return nil, NewInternal(err)
	}
	effective := make(map[string]any, len(s.registry))
	sections := make([]ConfigSection, 0, len(configSections))
	for _, sec := range configSections {
		secView := ConfigSection{Name: sec.name, Description: sec.description, Keys: []ConfigKeyView{}}
		for i := range s.registry {
			spec := &s.registry[i]
			if spec.Section != sec.name {
				continue
			}
			r, err := s.resolve(spec, overrides)
			if err != nil {
				return nil, err
			}
			dv, err := s.defaultOf(spec)
			if err != nil {
				return nil, err
			}
			effective[spec.Key] = r.Value
			secView.Keys = append(secView.Keys, ConfigKeyView{
				Key:              spec.Key,
				Section:          spec.Section,
				Type:             string(spec.Type),
				Description:      spec.Description,
				CurrentValue:     r.Value,
				DefaultValue:     dv,
				Source:           r.Source,
				SourceDetail:     r.Detail,
				DefaultIsDerived: spec.DefaultIsDerived,
				RequiresRestart:  spec.RequiresRestart,
				Editable:         spec.Editable,
			})
		}
		sections = append(sections, secView)
	}
	return &ListConfigResponse{
		Meta: ConfigMeta{
			Project:       "team",
			Instance:      "local",
			Version:       daemonVersion,
			UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
			GitCommit:     s.gitCommit,
			GitBranch:     s.gitBranch,
			ConfigHash:    configHash(effective),
			StartedAt:     s.startedAt.Format(time.RFC3339),
		},
		Sections:    sections,
		Environment: s.envViews(overrides),
	}, nil
}

// SetOverride — PUT /api/v1/config/{key}: тип-валидация + запись override в БД.
func (s *ConfigService) SetOverride(ctx context.Context, key string, raw json.RawMessage) (*SetOverrideResult, error) {
	spec, ok := s.byKey[key]
	if !ok {
		return nil, NewNotFound("config key " + key)
	}
	if !spec.Editable {
		return nil, &AppError{Code: "forbidden", Status: 403,
			Message: "config key " + key + " is not editable (secret or constant)"}
	}
	canonical, typed, err := parseOverrideValue(spec, raw)
	if err != nil {
		return nil, err
	}
	// текущее эффективное значение ДО установки (для needs_restart)
	overrides, err := s.repo.All(ctx, s.db)
	if err != nil {
		return nil, NewInternal(err)
	}
	cur, err := s.resolve(spec, overrides)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Upsert(ctx, s.db, key, canonical); err != nil {
		return nil, NewInternal(err)
	}
	return &SetOverrideResult{
		Key:             key,
		Value:           typed,
		Source:          SourceOverride,
		RequiresRestart: spec.RequiresRestart,
		NeedsRestart:    spec.RequiresRestart && cur.Raw != canonical,
	}, nil
}

// ResetOverride — DELETE /api/v1/config/{key}: сброс override (идемпотентно).
func (s *ConfigService) ResetOverride(ctx context.Context, key string) (*ResetOverrideResult, error) {
	spec, ok := s.byKey[key]
	if !ok {
		return nil, NewNotFound("config key " + key)
	}
	if err := s.repo.Delete(ctx, s.db, key); err != nil {
		return nil, NewInternal(err)
	}
	overrides, err := s.repo.All(ctx, s.db)
	if err != nil {
		return nil, NewInternal(err)
	}
	r, err := s.resolve(spec, overrides)
	if err != nil {
		return nil, err
	}
	return &ResetOverrideResult{Key: key, Source: r.Source, Value: r.Value}, nil
}

// parseOverrideValue — JSON-value → (каноническая строка, типизированное) по типу ключа.
// Неверный тип → 400 validation_failed.
func parseOverrideValue(spec *KeySpec, raw json.RawMessage) (string, any, error) {
	typErr := func() error {
		return NewValidation(fmt.Sprintf("value type mismatch for key %q: expected %s", spec.Key, spec.Type))
	}
	switch spec.Type {
	case StrType, PathType:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", nil, typErr()
		}
		return v, v, nil
	case BoolType:
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", nil, typErr()
		}
		return strconv.FormatBool(v), v, nil
	case IntType:
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil || f != math.Trunc(f) {
			return "", nil, typErr()
		}
		n := int64(f)
		return strconv.FormatInt(n, 10), n, nil
	case DurType:
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			if d, perr := time.ParseDuration(str); perr == nil {
				return d.String(), d.String(), nil
			}
			if d, perr := time.ParseDuration(str + "s"); perr == nil {
				return d.String(), d.String(), nil
			}
			return "", nil, typErr()
		}
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return "", nil, typErr()
		}
		d := time.Duration(f * float64(time.Second))
		return d.String(), d.String(), nil
	case StrListType:
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", nil, typErr()
		}
		return strings.Join(v, ","), v, nil
	default:
		return "", nil, typErr()
	}
}

// configHash — sha256 над отсортированным JSON эффективных значений всех ключей.
// (encoding/json сортирует keys мапы — JSON детерминирован).
func configHash(effective map[string]any) string {
	b, err := json.Marshal(effective)
	if err != nil {
		return "sha256:"
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// envViews — секция Environment: DAEMON_* переменные, влияющие на конфиг
// (provenance; secret-значения маскируются).
func (s *ConfigService) envViews(overrides map[string]string) []EnvVarView {
	out := []EnvVarView{}
	for i := range s.registry {
		spec := &s.registry[i]
		if spec.EnvVar == "" {
			continue
		}
		v, ok := s.envSnapshot[spec.EnvVar]
		if !ok || v == "" {
			continue
		}
		active := false
		if r, err := s.resolve(spec, overrides); err == nil {
			active = r.Source == SourceEnv
		}
		value := v
		if spec.Secret {
			if strings.HasPrefix(v, "sk") {
				value = v[:2] + "…/***"
			} else {
				value = "***"
			}
		}
		out = append(out, EnvVarView{EnvVar: spec.EnvVar, Value: value, FeedsKey: spec.Key, SourceActive: active})
	}
	return out
}

// gitInfo — git-хед/ветка cwd ("" если не репозиторий/нет git).
func gitInfo() (string, string) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	commit, branch := "", ""
	if b, err := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(b))
	}
	if b, err := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(b))
	}
	return commit, branch
}

// daemonVersion — версия демона (build-info подключается отдельно).
const daemonVersion = "1.0.0"
