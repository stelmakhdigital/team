
# Pi агент

Для запуска Pi агентов с гибкой конфигурацией (плагины, skills, настройки) лучше всего подойдёт **комбинированный подход**: tmux для интерактивности + система конфигураций через AgentSpec.


## Архитектура запуска Pi-агентов

```
┌──────────────────────────────────────────────────────────┐
│                     DAEMON                               │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Session Manager                                   │ │
│  │                                                    │ │
│  │  1. Читает AgentSpec (agent.yaml)                │ │
│  │  2. Генерирует конфиг для Pi                     │ │
│  │  3. Запускает tmux сессию с Pi                   │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
                          │
                          │
                          ▼
┌──────────────────────────────────────────────────────────┐
│              TMUX SESSION                                │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Oh My Pi Runtime                                  │ │
│  │                                                    │ │
│  │  Environment:                                      │ │
│  │  - PI_CONFIG=/path/to/generated-config.yaml      │ │
│  │  - PI_PLUGINS=plugin1,plugin2,plugin3            │ │
│  │  - PI_SKILLS=/path/to/skills                     │ │
│  │  - PI_WORKSPACE=/path/to/project                 │ │
│  │                                                    │ │
│  │  Startup:                                          │ │
│  │  1. Load config from PI_CONFIG                   │ │
│  │  2. Load plugins from PI_PLUGINS                 │ │
│  │  3. Load skills from PI_SKILLS                   │ │
│  │  4. Connect to LLM                               │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```


______________________________________________________________________

## 1. Структура AgentSpec для Pi

**Файл `agents/pi-go-backend/agent.yaml`:**

```yaml
# agents/pi-go-backend/agent.yaml

name: pi-go-backend
version: 0.1.0
description: Pi-агент для разработки бэкенда на Go

# Рантайм
runtime:
  type: pi  # или "oh-my-pi"
  version: ">=0.5.0"

# Конфигурация Pi
pi_config:
  # Базовые настройки
  model: "claude-3-7-sonnet"  # или "gpt-4.1", "local-llm"
  temperature: 0.7
  max_tokens: 4096
  
  # Плагины
  plugins:
    - name: git
      enabled: true
      config:
        auto_commit: false
        auto_push: false
    
    - name: github
      enabled: true
      config:
        repo: "myorg/myproject"
        auto_create_pr: false
    
    - name: terminal
      enabled: true
      config:
        allowed_commands:
          - "go"
          - "git"
          - "docker"
          - "make"
        forbidden_commands:
          - "rm -rf /"
          - "sudo"
    
    - name: filesystem
      enabled: true
      config:
        allowed_paths:
          - "/workspace/myproject"
        forbidden_paths:
          - "/etc"
          - "/root"
    
    - name: http
      enabled: true
      config:
        allowed_hosts:
          - "api.github.com"
          - "localhost"
  
  # Skills (SOPs)
  skills:
    - path: skills/code-review
      enabled: true
    - path: skills/tdd
      enabled: true
    - path: skills/go-style
      enabled: true
    - path: skills/openrig-basics
      enabled: true
  
  # Hooks
  hooks:
    pre_task:
      - path: hooks/check-git-status.sh
    post_task:
      - path: hooks/run-tests.sh
  
  # MCP серверы
  mcp:
    servers:
      - name: local-mcp
        url: "http://localhost:8080"
        enabled: true
  
  # Профили (переопределения)
  profiles:
    - name: project-x
      overrides:
        model: "claude-3-7-sonnet"
        plugins:
          github:
            repo: "myorg/project-x"
        skills:
          - path: skills/project-x-specific
            enabled: true

# Ресурсы для сессии
resources:
  cpu: "1.0"
  memory: "1G"
  gpu: false  # true если локальная LLM

# Startup файлы
startup:
  files:
    - path: guidance/role.md
      orientation: role
      delivery_hint: send_text
    - path: guidance/context.md
      orientation: context
      delivery_hint: project_file
```


______________________________________________________________________

## 2. Pi Adapter для tmux

**Создаём адаптер специально для Pi:**

```go
package runtime

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "text/template"
    "time"
    
    "gopkg.in/yaml.v3"
)

type PiAdapterConfig struct {
    TMUXBinary     string  // "tmux"
    SocketName     string  // "daemon"
    PiBinary       string  // "pi" или полный путь
    WorkspacesDir  string  // "/workspaces"
    ConfigsDir     string  // "/tmp/daemon/pi-configs"
    DefaultModel   string  // "claude-3-7-sonnet"
}

type PiAdapter struct {
    config PiAdapterConfig
}

func NewPiAdapter(cfg PiAdapterConfig) *PiAdapter {
    // Создать директорию для конфигов
    os.MkdirAll(cfg.ConfigsDir, 0755)
    
    return &PiAdapter{
        config: cfg,
    }
}

// PiSessionConfig - конфигурация для запуска Pi
type PiSessionConfig struct {
    SessionID     int64
    TaskID        int64
    Role          string
    AgentSpec     string  // путь к agent.yaml
    Profile       string  // профиль из agent_spec
    WorkingDir    string
    TargetDir     string  // корень проекта
    
    // Из AgentSpec
    Model         string
    Plugins       []PluginConfig
    Skills        []SkillConfig
    MCP           MCPConfig
    Environment   map[string]string
}

type PluginConfig struct {
    Name    string                 `yaml:"name"`
    Enabled bool                   `yaml:"enabled"`
    Config  map[string]interface{} `yaml:"config"`
}

type SkillConfig struct {
    Path    string `yaml:"path"`
    Enabled bool   `yaml:"enabled"`
}

type MCPConfig struct {
    Servers []MCPServer `yaml:"servers"`
}

type MCPServer struct {
    Name    string `yaml:"name"`
    URL     string `yaml:"url"`
    Enabled bool   `yaml:"enabled"`
}
```

**Запуск Pi сессии:**

```go
func (a *PiAdapter) Start(ctx context.Context, cfg SessionConfig) (string, error) {
    // 1. Сформировать имя сессии
    sessionName := fmt.Sprintf("pi-%d", cfg.SessionID)
    
    // 2. Загрузить AgentSpec
    agentSpec, err := a.loadAgentSpec(cfg.AgentSpec)
    if err != nil {
        return "", fmt.Errorf("failed to load agent spec: %w", err)
    }
    
    // 3. Применить профиль (если указан)
    if cfg.Profile != "" {
        if err := a.applyProfile(agentSpec, cfg.Profile); err != nil {
            return "", fmt.Errorf("failed to apply profile: %w", err)
        }
    }
    
    // 4. Сгенерировать конфиг для Pi
    piConfig, err := a.generatePiConfig(agentSpec, cfg)
    if err != nil {
        return "", fmt.Errorf("failed to generate pi config: %w", err)
    }
    
    // 5. Сохранить конфиг во временный файл
    configPath := filepath.Join(a.config.ConfigsDir, fmt.Sprintf("session-%d.yaml", cfg.SessionID))
    
    if err := a.savePiConfig(configPath, piConfig); err != nil {
        return "", fmt.Errorf("failed to save pi config: %w", err)
    }
    
    // 6. Подготовить environment для Pi
    env := a.buildEnvironment(cfg, agentSpec, configPath)
    
    // 7. Сформировать команду для запуска Pi
    piCmd := a.buildPiCommand(cfg, agentSpec, configPath)
    
    // 8. Создать tmux сессию
    if err := a.createTMUXSession(sessionName, piCmd, env, cfg.WorkingDir); err != nil {
        return "", fmt.Errorf("failed to create tmux session: %w", err)
    }
    
    // 9. Загрузить startup файлы (guidance, skills)
    if err := a.loadStartupFiles(cfg, agentSpec); err != nil {
        log.Warn("failed to load startup files", "err", err)
    }
    
    // 10. Вернуть runtimeRef
    return sessionName, nil
}

func (a *PiAdapter) loadAgentSpec(path string) (*AgentSpec, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    
    var spec AgentSpec
    if err := yaml.Unmarshal(data, &spec); err != nil {
        return nil, err
    }
    
    return &spec, nil
}

func (a *PiAdapter) generatePiConfig(spec *AgentSpec, cfg SessionConfig) (*PiConfig, error) {
    piCfg := &PiConfig{
        Model:       spec.PiConfig.Model,
        Temperature: spec.PiConfig.Temperature,
        MaxTokens:   spec.PiConfig.MaxTokens,
        
        WorkingDir:  cfg.WorkingDir,
        TargetDir:   cfg.TargetDir,
        
        SessionID:   cfg.SessionID,
        TaskID:      cfg.TaskID,
        Role:        cfg.Role,
    }
    
    // Плагины
    for _, plugin := range spec.PiConfig.Plugins {
        if plugin.Enabled {
            piCfg.Plugins = append(piCfg.Plugins, PluginConfig{
                Name:    plugin.Name,
                Enabled: true,
                Config:  plugin.Config,
            })
        }
    }
    
    // Skills
    for _, skill := range spec.PiConfig.Skills {
        if skill.Enabled {
            piCfg.Skills = append(piCfg.Skills, SkillConfig{
                Path:    skill.Path,
                Enabled: true,
            })
        }
    }
    
    // MCP
    piCfg.MCP = spec.PiConfig.MCP
    
    return piCfg, nil
}

func (a *PiAdapter) savePiConfig(path string, cfg *PiConfig) error {
    data, err := yaml.Marshal(cfg)
    if err != nil {
        return err
    }
    
    return os.WriteFile(path, data, 0644)
}

func (a *PiAdapter) buildEnvironment(cfg SessionConfig, spec *AgentSpec, configPath string) map[string]string {
    env := make(map[string]string)
    
    // Базовые переменные
    env["SESSION_ID"] = fmt.Sprintf("%d", cfg.SessionID)
    env["TASK_ID"] = fmt.Sprintf("%d", cfg.TaskID)
    env["ROLE"] = cfg.Role
    env["AGENT_SPEC"] = cfg.AgentSpec
    env["PROFILE"] = cfg.Profile
    
    // Pi-specific
    env["PI_CONFIG"] = configPath
    env["PI_MODEL"] = spec.PiConfig.Model
    env["PI_WORKSPACE"] = cfg.WorkingDir
    env["PI_TARGET"] = cfg.TargetDir
    
    // Плагины (список)
    var pluginNames []string
    for _, plugin := range spec.PiConfig.Plugins {
        if plugin.Enabled {
            pluginNames = append(pluginNames, plugin.Name)
        }
    }
    env["PI_PLUGINS"] = strings.Join(pluginNames, ",")
    
    // Skills (путь)
    var skillPaths []string
    for _, skill := range spec.PiConfig.Skills {
        if skill.Enabled {
            skillPaths = append(skillPaths, skill.Path)
        }
    }
    env["PI_SKILLS"] = strings.Join(skillPaths, ":")
    
    // Custom environment из spec
    for k, v := range cfg.Environment {
        env[k] = v
    }
    
    return env
}

func (a *PiAdapter) buildPiCommand(cfg SessionConfig, spec *AgentSpec, configPath string) string {
    // Базовая команда
    cmd := fmt.Sprintf("%s --config %s", a.config.PiBinary, configPath)
    
    // Дополнительные флаги
    if cfg.WorkingDir != "" {
        cmd += fmt.Sprintf(" --working-dir %s", cfg.WorkingDir)
    }
    
    if cfg.TargetDir != "" {
        cmd += fmt.Sprintf(" --target %s", cfg.TargetDir)
    }
    
    // Session metadata
    cmd += fmt.Sprintf(" --session-id %d", cfg.SessionID)
    cmd += fmt.Sprintf(" --task-id %d", cfg.TaskID)
    cmd += fmt.Sprintf(" --role %s", cfg.Role)
    
    return cmd
}

func (a *PiAdapter) createTMUXSession(sessionName, command string, env map[string]string, workingDir string) error {
    // 1. Сформировать команду с environment
    var envCmd strings.Builder
    
    for k, v := range env {
        envCmd.WriteString(fmt.Sprintf("%s=\"%s\" ", k, v))
    }
    
    fullCmd := envCmd.String() + command
    
    // 2. Создать tmux сессию
    cmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "new-session",
        "-d",  // detached
        "-s", sessionName,
        "-c", workingDir,  // working directory
        fullCmd,
    )
    
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to create tmux session: %w", err)
    }
    
    // 3. Подождать немного, чтобы сессия инициализировалась
    time.Sleep(2 * time.Second)
    
    // 4. Проверить, что сессия активна
    statusCmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "has-session",
        "-t", sessionName,
    )
    
    if err := statusCmd.Run(); err != nil {
        return fmt.Errorf("tmux session failed to start: %w", err)
    }
    
    return nil
}

func (a *PiAdapter) loadStartupFiles(cfg SessionConfig, spec *AgentSpec) error {
    // Загрузить startup файлы из spec
    for _, file := range spec.Startup.Files {
        data, err := os.ReadFile(file.Path)
        if err != nil {
            log.Warn("failed to read startup file", "path", file.Path, "err", err)
            continue
        }
        
        // Отправить файл в сессию
        if err := a.sendToSession(cfg.SessionID, file.Orientation, string(data)); err != nil {
            log.Warn("failed to send startup file", "path", file.Path, "err", err)
        }
    }
    
    return nil
}

func (a *PiAdapter) sendToSession(sessionID int64, orientation, content string) error {
    sessionName := fmt.Sprintf("pi-%d", sessionID)
    
    // В зависимости от orientation:
    // - role: отправить как сообщение
    // - context: сохранить в файл проекта
    
    if orientation == "role" {
        // Отправить как текст в чат
        cmd := exec.Command(
            a.config.TMUXBinary,
            "-L", a.config.SocketName,
            "send-keys",
            "-t", sessionName,
            fmt.Sprintf("C-c"),  // Ctrl+C чтобы прервать текущую команду
        )
        cmd.Run()
        
        cmd = exec.Command(
            a.config.TMUXBinary,
            "-L", a.config.SocketName,
            "send-keys",
            "-t", sessionName,
            fmt.Sprintf("# ROLE: %s", content),
            "Enter",
        )
        return cmd.Run()
    }
    
    return nil
}
```


______________________________________________________________________

## 3. Интеграция с Session Manager

**Добавляем Pi adapter в менеджер:**

```go
func NewSessionManager(cfg ManagerConfig, repo SessionRepository) (*SessionManager, error) {
    adapters := make(map[runtime.Type]runtime.RuntimeAdapter)
    
    // ... другие адаптеры ...
    
    // Pi adapter
    if cfg.Pi.TMUXBinary != "" {
        piAdapter := runtime.NewPiAdapter(cfg.Pi)
        adapters[runtime.TypePi] = piAdapter
    }
    
    return &SessionManager{
        adapters: adapters,
        repo:     repo,
    }, nil
}
```

**Запуск Pi сессии:**

```go
// В Task Orchestrator
func (o *TaskOrchestrator) startPiSession(ctx context.Context, task *Task, role *Role, agentSpecPath, profile string) error {
    session, err := o.sessionManager.StartSession(ctx, StartSessionRequest{
        TeamID:      task.TeamID,
        RoleID:      role.ID,
        QueueTaskID: task.ID,
        RuntimeType: "pi",  // специальный тип для Pi
        Role:        role.Name,
        AgentType:   "pi-coder",
        WorkingDir:  "/workspace/myproject",
        TargetDir:   "/projects/myproject",
        AgentSpec:   agentSpecPath,
        Profile:     profile,
        CPU:         "1.0",
        Memory:      "1G",
    })
    
    if err != nil {
        return err
    }
    
    // Обновить задачу
    task.SessionID = session.ID
    return o.taskRepo.Update(ctx, task)
}
```


______________________________________________________________________

## 4. Динамическое конфигурирование

**Можно переопределять конфиг на лету:**

```go
// API endpoint для обновления конфигурации сессии
type UpdatePiConfigRequest struct {
    SessionID   int64                  `json:"session_id"`
    Plugins     map[string]bool        `json:"plugins"`  // name -> enabled
    Model       string                 `json:"model"`
    Temperature float64                `json:"temperature"`
    Skills      []string               `json:"skills"`   // paths to add
}

func (h *SessionHandler) UpdatePiConfig(w http.ResponseWriter, r *http.Request) {
    var req UpdatePiConfigRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    // 1. Получить сессию
    session, err := h.sessionRepo.GetByID(r.Context(), req.SessionID)
    if err != nil {
        http.Error(w, err.Error(), http.StatusNotFound)
        return
    }
    
    // 2. Загрузить текущий конфиг
    configPath := filepath.Join(h.piAdapter.ConfigsDir, fmt.Sprintf("session-%d.yaml", req.SessionID))
    
    piCfg, err := h.piAdapter.loadPiConfig(configPath)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // 3. Применить изменения
    if req.Model != "" {
        piCfg.Model = req.Model
    }
    
    if req.Temperature > 0 {
        piCfg.Temperature = req.Temperature
    }
    
    // Обновить плагины
    for name, enabled := range req.Plugins {
        for i, plugin := range piCfg.Plugins {
            if plugin.Name == name {
                piCfg.Plugins[i].Enabled = enabled
            }
        }
    }
    
    // 4. Сохранить обновлённый конфиг
    if err := h.piAdapter.savePiConfig(configPath, piCfg); err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // 5. Отправить сигнал Pi перезагрузить конфиг
    sessionName := fmt.Sprintf("pi-%d", req.SessionID)
    cmd := exec.Command(
        h.piAdapter.config.TMUXBinary,
        "-L", h.piAdapter.config.SocketName,
        "send-keys",
        "-t", sessionName,
        "C-c",  // Ctrl+C
        "Enter",
        "pi --reload-config",  // команда для перезагрузки
        "Enter",
    )
    
    if err := cmd.Run(); err != nil {
        log.Warn("failed to reload config", "err", err)
    }
    
    json.NewEncoder(w).Encode(map[string]string{
        "status": "ok",
    })
}
```


______________________________________________________________________

## 5. Пример использования

**Создание задачи с Pi-агентом:**

```go
// 1. Создать задачу
task, err := taskService.CreateTask(ctx, CreateTaskRequest{
    TeamID:      team.ID,
    ProjectID:   project.ID,
    Title:       "Реализовать OAuth2 авторизацию",
    Description: "Нужно добавить авторизацию через Google OAuth2",
    Role:        "pi-go-backend",  // тип агента
})

// 2. Запустить Pi сессию
err = orchestrator.startPiSession(ctx, task, role, 
    "agents/pi-go-backend/agent.yaml",  // AgentSpec
    "project-x",  // профиль
)

// 3. Pi запускается с конфигом:
// - модель: claude-3-7-sonnet
// - плагины: git, github, terminal, filesystem
// - skills: code-review, tdd, go-style
// - MCP: local-mcp
```


______________________________________________________________________

## 6. Управление плагинами

**API для включения/выключения плагинов:**

```bash
# Включить github плагин
curl -X POST http://localhost:8080/api/v1/sessions/123/pi/plugins \
  -H "Content-Type: application/json" \
  -d '{
    "github": {
      "enabled": true,
      "config": {
        "repo": "myorg/myproject",
        "auto_create_pr": true
      }
    }
  }'

# Выключить terminal плагин
curl -X POST http://localhost:8080/api/v1/sessions/123/pi/plugins \
  -H "Content-Type: application/json" \
  -d '{
    "terminal": {
      "enabled": false
    }
  }'
```


______________________________________________________________________

## 7. Мониторинг Pi сессий

```go
func (a *PiAdapter) Status(ctx context.Context, runtimeRef string) (SessionState, error) {
    sessionName := runtimeRef  // "pi-123"
    
    // Проверить, существует ли tmux сессия
    cmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "has-session",
        "-t", sessionName,
    )
    
    if err := cmd.Run(); err != nil {
        return StateStopped, nil
    }
    
    // Проверить, активен ли Pi (можно через ping в API Pi)
    // Или проверить логи на наличие ошибок
    
    return StateRunning, nil
}

func (a *PiAdapter) GetLogs(ctx context.Context, runtimeRef string, tail int) ([]string, error) {
    sessionName := runtimeRef
    
    cmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "capture-pane",
        "-t", sessionName,
        "-p",
    )
    
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }
    
    lines := strings.Split(string(output), "\n")
    
    if len(lines) > tail {
        lines = lines[len(lines)-tail:]
    }
    
    return lines, nil
}
```


______________________________________________________________________

## Рекомендации

### 1. **AgentSpec как единый источник правды**

- Вся конфигурация в `agent.yaml`
- Версионировать spec вместе с кодом
- Валидировать spec при загрузке


### 2. **Профили для разных сценариев**

- `default` — базовая конфигурация
- `project-x` — специфика проекта
- `debug` — с дополнительным логированием
- `production` — с ограничениями


### 3. **Безопасность**

- Ограничивать allowed_commands в terminal плагине
- Запрещать доступ к чувствительным путям
- Валидировать MCP серверы


### 4. **Отладка**

- Логировать сгенерированный конфиг
- Сохранять конфиги после запуска
- Добавить endpoint для просмотра текущего конфига

