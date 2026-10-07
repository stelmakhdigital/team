
# Runtime adapters — как запускать контейнеры/процессы


## Архитектура Runtime Adapters

```
┌─────────────────────────────────────────────────────────┐
│                  Session Manager                        │
│                                                         │
│  ┌───────────────────────────────────────────────────┐ │
│  │  RuntimeAdapter (interface)                       │ │
│  │                                                   │ │
│  │  Start(ctx, cfg) -> (runtimeRef, error)          │ │
│  │  Stop(ctx, runtimeRef) -> error                  │ │
│  │  Status(ctx, runtimeRef) -> (SessionState, err)  │ │
│  │  SendCommand(ctx, runtimeRef, cmd) -> error      │ │
│  └───────────────────────────────────────────────────┘ │
│                      │                                  │
│         ┌────────────┼────────────┐                    │
│         │            │            │                    │
│         ▼            ▼            ▼                    │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐            │
│  │ Container│  │ Process  │  │   TMUX   │            │
│  │ Adapter  │  │  Adapter │  │  Adapter │            │
│  └──────────┘  └──────────┘  └──────────┘            │
│         │            │            │                    │
│         ▼            ▼            ▼                    │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐            │
│  │  Docker  │  │   os.    │  │  tmux    │            │
│  │   SDK    │  │  exec    │  │  CLI     │            │
│  └──────────┘  └──────────┘  └──────────┘            │
└─────────────────────────────────────────────────────────┘
```


______________________________________________________________________

## Интерфейс RuntimeAdapter

**Базовый интерфейс:**

```go
package runtime

import (
    "context"
    "time"
)

// SessionConfig - конфигурация для запуска сессии
type SessionConfig struct {
    // Идентификаторы
    SessionID   int64
    TaskID      int64
    Role        string  // "lead", "worker", "watchdog"
    AgentType   string  // "llm_go_coder", "llm_reviewer", etc.
    
    // Рабочая директория
    WorkingDir  string
    TargetDir   string  // корень проекта (репозиторий)
    
    // Агент
    AgentSpec   string  // путь к agent.yaml
    Profile     string  // профиль из agent_spec
    
    // Окружение
    Environment map[string]string
    
    // Ресурсы
    CPU         string  // "1.0"
    Memory      string  // "512M"
    
    // Таймауты
    Timeout     time.Duration
    
    // Контекст задачи
    TaskContext map[string]interface{}
}

// SessionState - состояние сессии
type SessionState string

const (
    StateStarting   SessionState = "starting"
    StateRunning    SessionState = "running"
    StateIdle       SessionState = "idle"
    StateStopping   SessionState = "stopping"
    StateStopped    SessionState = "stopped"
    StateFailed     SessionState = "failed"
)

// RuntimeAdapter - интерфейс для управления сессиями
type RuntimeAdapter interface {
    // Start запускает сессию и возвращает runtimeRef (идентификатор в runtime)
    Start(ctx context.Context, cfg SessionConfig) (runtimeRef string, err error)
    
    // Stop останавливает сессию
    Stop(ctx context.Context, runtimeRef string) error
    
    // Status возвращает текущее состояние сессии
    Status(ctx context.Context, runtimeRef string) (SessionState, error)
    
    // SendCommand отправляет команду в сессию
    SendCommand(ctx context.Context, runtimeRef string, cmd string) error
    
    // GetLogs получает логи сессии
    GetLogs(ctx context.Context, runtimeRef string, tail int) ([]string, error)
}
```


______________________________________________________________________

## 1. Container Adapter (Docker)

**Конфигурация:**

```go
package runtime

import (
    "context"
    "encoding/json"
    "fmt"
    "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/container"
    "github.com/docker/docker/api/types/network"
    "github.com/docker/docker/client"
    "github.com/docker/go-connections/nat"
    "io"
    "time"
)

type ContainerAdapterConfig struct {
    DockerSocket   string  // "/var/run/docker.sock"
    Network        string  // "daemon-network"
    Image          string  // "agent-runtime:latest"
    WorkingDir     string  // "/workspace"
    EnableGPU      bool    // false
    GPUDevices     []string
}

type ContainerAdapter struct {
    config     ContainerAdapterConfig
    dockerCli  *client.Client
}

func NewContainerAdapter(cfg ContainerAdapterConfig) (*ContainerAdapter, error) {
    cli, err := client.NewClientWithOpts(
        client.WithHost(cfg.DockerSocket),
        client.WithAPIVersionNegotiation(),
    )
    
    if err != nil {
        return nil, fmt.Errorf("failed to create docker client: %w", err)
    }
    
    return &ContainerAdapter{
        config:    cfg,
        dockerCli: cli,
    }, nil
}
```

**Запуск контейнера:**

```go
func (a *ContainerAdapter) Start(ctx context.Context, cfg SessionConfig) (string, error) {
    // 1. Сформировать имя контейнера
    containerName := fmt.Sprintf("agent-session-%d", cfg.SessionID)
    
    // 2. Подготовить environment variables
    env := []string{
        fmt.Sprintf("SESSION_ID=%d", cfg.SessionID),
        fmt.Sprintf("TASK_ID=%d", cfg.TaskID),
        fmt.Sprintf("ROLE=%s", cfg.Role),
        fmt.Sprintf("AGENT_TYPE=%s", cfg.AgentType),
        fmt.Sprintf("AGENT_SPEC=%s", cfg.AgentSpec),
        fmt.Sprintf("PROFILE=%s", cfg.Profile),
        fmt.Sprintf("WORKING_DIR=%s", cfg.WorkingDir),
        fmt.Sprintf("TARGET_DIR=%s", cfg.TargetDir),
    }
    
    // Добавить custom environment
    for k, v := range cfg.Environment {
        env = append(env, fmt.Sprintf("%s=%s", k, v))
    }
    
    // 3. Подготовить volumes
    binds := []string{
        fmt.Sprintf("%s:%s:rw", cfg.TargetDir, cfg.TargetDir),  // проект
    }
    
    // 4. Конфигурация контейнера
    containerCfg := &container.Config{
        Image:        a.config.Image,
        Cmd:          []string{"/usr/bin/agent-runtime", "--session-id", fmt.Sprintf("%d", cfg.SessionID)},
        Env:          env,
        WorkingDir:   a.config.WorkingDir,
        Labels: map[string]string{
            "daemon.session_id":   fmt.Sprintf("%d", cfg.SessionID),
            "daemon.task_id":      fmt.Sprintf("%d", cfg.TaskID),
            "daemon.role":         cfg.Role,
            "managed-by":          "daemon",
        },
        AttachStdin:  true,
        AttachStdout: true,
        AttachStderr: true,
        Tty:          true,
        OpenStdin:    true,
    }
    
    // 5. Host configuration
    hostCfg := &container.HostConfig{
        AutoRemove:  false,
        Binds:       binds,
        NetworkMode: container.NetworkMode(a.config.Network),
        Resources: container.Resources{
            Memory: a.parseMemory(cfg.Memory),
            NanoCPUs: a.parseCPU(cfg.CPU),
        },
    }
    
    // GPU поддержка (опционально)
    if a.config.EnableGPU && cfg.AgentType == "llm_local" {
        hostCfg.DeviceRequests = []container.DeviceRequest{
            {
                Driver:       "nvidia",
                Count:        -1,  // все GPU
                Capabilities: [][]string{{"gpu"}},
            },
        }
    }
    
    // 6. Network configuration
    networkCfg := &network.NetworkingConfig{}
    
    // 7. Создать контейнер
    createResp, err := a.dockerCli.ContainerCreate(
        ctx,
        containerCfg,
        hostCfg,
        networkCfg,
        nil,
        containerName,
    )
    
    if err != nil {
        return "", fmt.Errorf("failed to create container: %w", err)
    }
    
    // 8. Запустить контейнер
    if err := a.dockerCli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
        // Очистить при ошибке запуска
        a.dockerCli.ContainerRemove(ctx, createResp.ID, container.RemoveOptions{Force: true})
        return "", fmt.Errorf("failed to start container: %w", err)
    }
    
    // 9. Вернуть runtimeRef (ID контейнера)
    return createResp.ID, nil
}

func (a *ContainerAdapter) parseMemory(mem string) int64 {
    if mem == "" {
        return 512 * 1024 * 1024  // 512MB по умолчанию
    }
    
    // Парсинг: "512M", "1G", etc.
    // Упрощённая реализация
    switch mem {
    case "256M":
        return 256 * 1024 * 1024
    case "512M":
        return 512 * 1024 * 1024
    case "1G":
        return 1024 * 1024 * 1024
    case "2G":
        return 2 * 1024 * 1024 * 1024
    default:
        return 512 * 1024 * 1024
    }
}

func (a *ContainerAdapter) parseCPU(cpu string) int64 {
    if cpu == "" {
        return 1000000000  // 1.0 CPU
    }
    
    // Парсинг: "0.5", "1.0", "2.0"
    // Упрощённая реализация
    switch cpu {
    case "0.5":
        return 500000000
    case "1.0":
        return 1000000000
    case "2.0":
        return 2000000000
    default:
        return 1000000000
    }
}
```

**Остановка контейнера:**

```go
func (a *ContainerAdapter) Stop(ctx context.Context, runtimeRef string) error {
    // 1. Graceful stop (10 секунд)
    timeout := 10 * time.Second
    
    err := a.dockerCli.ContainerStop(ctx, runtimeRef, container.StopOptions{
        Timeout: &timeout,
    })
    
    if err != nil {
        return fmt.Errorf("failed to stop container: %w", err)
    }
    
    // 2. Удалить контейнер (опционально)
    // Можно не удалять для отладки
    removeErr := a.dockerCli.ContainerRemove(ctx, runtimeRef, container.RemoveOptions{
        Force: true,
    })
    
    if removeErr != nil {
        return fmt.Errorf("failed to remove container: %w", removeErr)
    }
    
    return nil
}
```

**Проверка статуса:**

```go
func (a *ContainerAdapter) Status(ctx context.Context, runtimeRef string) (SessionState, error) {
    inspect, err := a.dockerCli.ContainerInspect(ctx, runtimeRef)
    if err != nil {
        if client.IsErrNotFound(err) {
            return StateStopped, nil
        }
        return StateFailed, fmt.Errorf("failed to inspect container: %w", err)
    }
    
    if !inspect.State.Running {
        if inspect.State.ExitCode == 0 {
            return StateStopped, nil
        }
        return StateFailed, fmt.Errorf("container exited with code %d", inspect.State.ExitCode)
    }
    
    return StateRunning, nil
}
```

**Отправка команды:**

```go
func (a *ContainerAdapter) SendCommand(ctx context.Context, runtimeRef string, cmd string) error {
    // 1. Создать exec
    execCfg := types.ExecConfig{
        Cmd:          []string{"/bin/sh", "-c", cmd},
        AttachStdout: true,
        AttachStderr: true,
        Tty:          true,
    }
    
    execResp, err := a.dockerCli.ContainerExecCreate(ctx, runtimeRef, execCfg)
    if err != nil {
        return fmt.Errorf("failed to create exec: %w", err)
    }
    
    // 2. Запустить exec
    execStartCheck := types.ExecStartCheck{
        Tty: true,
    }
    
    resp, err := a.dockerCli.ContainerExecAttach(ctx, execResp.ID, execStartCheck)
    if err != nil {
        return fmt.Errorf("failed to start exec: %w", err)
    }
    defer resp.Close()
    
    // 3. Прочитать вывод (опционально)
    _, err = io.Copy(io.Discard, resp.Reader)
    if err != nil {
        return fmt.Errorf("failed to read exec output: %w", err)
    }
    
    return nil
}
```

**Получение логов:**

```go
func (a *ContainerAdapter) GetLogs(ctx context.Context, runtimeRef string, tail int) ([]string, error) {
    options := container.LogsOptions{
        ShowStdout: true,
        ShowStderr: true,
        Tail:       fmt.Sprintf("%d", tail),
    }
    
    logs, err := a.dockerCli.ContainerLogs(ctx, runtimeRef, options)
    if err != nil {
        return nil, fmt.Errorf("failed to get logs: %w", err)
    }
    defer logs.Close()
    
    // Парсинг логов (Docker добавляет заголовки)
    var lines []string
    scanner := bufio.NewScanner(logs)
    
    for scanner.Scan() {
        // Пропустить 8 байт заголовка
        line := scanner.Bytes()
        if len(line) > 8 {
            lines = append(lines, string(line[8:]))
        }
    }
    
    return lines, scanner.Err()
}
```


______________________________________________________________________

## 2. Process Adapter (локальные процессы)

**Конфигурация:**

```go
package runtime

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "sync"
    "syscall"
    "time"
)

type ProcessAdapterConfig struct {
    WorkingDir     string  // "/tmp/daemon/sessions"
    RuntimeBinary  string  // "/usr/bin/agent-runtime"
    DefaultTimeout time.Duration
}

type ProcessSession struct {
    cmd        *exec.Cmd
    sessionID  int64
    startTime  time.Time
    mu         sync.Mutex
}

type ProcessAdapter struct {
    config     ProcessAdapterConfig
    sessions   map[string]*ProcessSession  // runtimeRef -> session
    mu         sync.RWMutex
}

func NewProcessAdapter(cfg ProcessAdapterConfig) *ProcessAdapter {
    // Создать рабочую директорию
    os.MkdirAll(cfg.WorkingDir, 0755)
    
    return &ProcessAdapter{
        config:   cfg,
        sessions: make(map[string]*ProcessSession),
    }
}
```

**Запуск процесса:**

```go
func (a *ProcessAdapter) Start(ctx context.Context, cfg SessionConfig) (string, error) {
    // 1. Создать директорию для сессии
    sessionDir := filepath.Join(a.config.WorkingDir, fmt.Sprintf("session-%d", cfg.SessionID))
    
    if err := os.MkdirAll(sessionDir, 0755); err != nil {
        return "", fmt.Errorf("failed to create session dir: %w", err)
    }
    
    // 2. Подготовить environment
    env := os.Environ()
    env = append(env,
        fmt.Sprintf("SESSION_ID=%d", cfg.SessionID),
        fmt.Sprintf("TASK_ID=%d", cfg.TaskID),
        fmt.Sprintf("ROLE=%s", cfg.Role),
        fmt.Sprintf("AGENT_TYPE=%s", cfg.AgentType),
        fmt.Sprintf("AGENT_SPEC=%s", cfg.AgentSpec),
        fmt.Sprintf("PROFILE=%s", cfg.Profile),
        fmt.Sprintf("WORKING_DIR=%s", cfg.WorkingDir),
        fmt.Sprintf("TARGET_DIR=%s", cfg.TargetDir),
    )
    
    for k, v := range cfg.Environment {
        env = append(env, fmt.Sprintf("%s=%s", k, v))
    }
    
    // 3. Создать команду
    cmd := exec.Command(
        a.config.RuntimeBinary,
        "--session-id", fmt.Sprintf("%d", cfg.SessionID),
        "--task-id", fmt.Sprintf("%d", cfg.TaskID),
        "--role", cfg.Role,
        "--working-dir", cfg.WorkingDir,
    )
    
    cmd.Env = env
    cmd.Dir = sessionDir
    cmd.Stdout = os.Stdout  // или логгер
    cmd.Stderr = os.Stderr
    
    // 4. Запустить процесс
    if err := cmd.Start(); err != nil {
        return "", fmt.Errorf("failed to start process: %w", err)
    }
    
    // 5. Создать runtimeRef
    runtimeRef := fmt.Sprintf("process-%d", cfg.SessionID)
    
    // 6. Сохранить сессию
    a.mu.Lock()
    a.sessions[runtimeRef] = &ProcessSession{
        cmd:       cmd,
        sessionID: cfg.SessionID,
        startTime: time.Now(),
    }
    a.mu.Unlock()
    
    // 7. Запустить goroutine для мониторинга
    go a.monitorProcess(runtimeRef, cmd)
    
    return runtimeRef, nil
}

func (a *ProcessAdapter) monitorProcess(runtimeRef string, cmd *exec.Cmd) {
    // Ждать завершения процесса
    err := cmd.Wait()
    
    a.mu.Lock()
    defer a.mu.Unlock()
    
    // Удалить из мапы
    delete(a.sessions, runtimeRef)
    
    if err != nil {
        log.Error("process exited with error", "runtimeRef", runtimeRef, "err", err)
    } else {
        log.Info("process exited normally", "runtimeRef", runtimeRef)
    }
}
```

**Остановка процесса:**

```go
func (a *ProcessAdapter) Stop(ctx context.Context, runtimeRef string) error {
    a.mu.RLock()
    session, ok := a.sessions[runtimeRef]
    a.mu.RUnlock()
    
    if !ok {
        return fmt.Errorf("session not found: %s", runtimeRef)
    }
    
    // 1. Graceful stop (SIGTERM)
    session.mu.Lock()
    defer session.mu.Unlock()
    
    if session.cmd.Process != nil {
        // Отправить SIGTERM
        if err := session.cmd.Process.Signal(syscall.SIGTERM); err != nil {
            // Если не удалось, отправить SIGKILL
            if err := session.cmd.Process.Kill(); err != nil {
                return fmt.Errorf("failed to kill process: %w", err)
            }
        }
        
        // Ждать завершения (с таймаутом)
        done := make(chan error, 1)
        
        go func() {
            done <- session.cmd.Wait()
        }()
        
        select {
        case <-time.After(10 * time.Second):
            // Таймаут, убить процесс
            session.cmd.Process.Kill()
        case <-done:
            // Процесс завершился
        }
    }
    
    // 2. Удалить директорию сессии (опционально)
    sessionDir := filepath.Join(a.config.WorkingDir, fmt.Sprintf("session-%d", session.sessionID))
    os.RemoveAll(sessionDir)
    
    return nil
}
```

**Проверка статуса:**

```go
func (a *ProcessAdapter) Status(ctx context.Context, runtimeRef string) (SessionState, error) {
    a.mu.RLock()
    session, ok := a.sessions[runtimeRef]
    a.mu.RUnlock()
    
    if !ok {
        return StateStopped, nil
    }
    
    // Проверить, жив ли процесс
    session.mu.Lock()
    defer session.mu.Unlock()
    
    if session.cmd.Process == nil {
        return StateStopped, nil
    }
    
    // Попытаться отправить сигнал 0 (проверка)
    if err := session.cmd.Process.Signal(syscall.Signal(0)); err != nil {
        return StateStopped, nil
    }
    
    return StateRunning, nil
}
```

**Отправка команды:**

```go
func (a *ProcessAdapter) SendCommand(ctx context.Context, runtimeRef string, cmd string) error {
    // Для процессов это сложнее, нужно IPC
    // Можно использовать:
    // 1. Named pipes (FIFO)
    // 2. Unix domain sockets
    // 3. HTTP API внутри процесса
    
    // Пример с HTTP API (если agent-runtime слушает localhost)
    sessionID := extractSessionID(runtimeRef)
    
    url := fmt.Sprintf("http://localhost:%d/command", 8080+sessionID)
    
    resp, err := http.Post(url, "application/json", strings.NewReader(cmd))
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    return nil
}
```


______________________________________________________________________

## 3. TMUX Adapter (терминальные сессии)

**Конфигурация:**

```go
package runtime

import (
    "context"
    "fmt"
    "os/exec"
    "strings"
)

type TMUXAdapterConfig struct {
    TMUXBinary   string  // "tmux"
    SocketName   string  // "daemon"
}

type TMUXAdapter struct {
    config TMUXAdapterConfig
}

func NewTMUXAdapter(cfg TMUXAdapterConfig) *TMUXAdapter {
    return &TMUXAdapter{
        config: cfg,
    }
}
```

**Запуск tmux сессии:**

```go
func (a *TMUXAdapter) Start(ctx context.Context, cfg SessionConfig) (string, error) {
    // 1. Сформировать имя сессии
    sessionName := fmt.Sprintf("agent-%d", cfg.SessionID)
    
    // 2. Подготовить команду для запуска
    runtimeCmd := fmt.Sprintf(
        "agent-runtime --session-id %d --task-id %d --role %s",
        cfg.SessionID,
        cfg.TaskID,
        cfg.Role,
    )
    
    // 3. Создать tmux сессию
    cmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "new-session",
        "-d",  // detached mode
        "-s", sessionName,
        runtimeCmd,
    )
    
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("failed to create tmux session: %w", err)
    }
    
    // 4. Вернуть runtimeRef
    return sessionName, nil
}
```

**Остановка tmux сессии:**

```go
func (a *TMUXAdapter) Stop(ctx context.Context, runtimeRef string) error {
    // 1. Отправить kill-session
    cmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "kill-session",
        "-t", runtimeRef,
    )
    
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to kill tmux session: %w", err)
    }
    
    return nil
}
```

**Проверка статуса:**

```go
func (a *TMUXAdapter) Status(ctx context.Context, runtimeRef string) (SessionState, error) {
    // 1. Проверить, существует ли сессия
    cmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "has-session",
        "-t", runtimeRef,
    )
    
    if err := cmd.Run(); err != nil {
        // Сессия не существует
        return StateStopped, nil
    }
    
    return StateRunning, nil
}
```

**Отправка команды:**

```go
func (a *TMUXAdapter) SendCommand(ctx context.Context, runtimeRef string, cmd string) error {
    // 1. Отправить команду в сессию
    tmuxCmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "send-keys",
        "-t", runtimeRef,
        cmd,
        "Enter",
    )
    
    if err := tmuxCmd.Run(); err != nil {
        return fmt.Errorf("failed to send command: %w", err)
    }
    
    return nil
}
```

**Получение логов:**

```go
func (a *TMUXAdapter) GetLogs(ctx context.Context, runtimeRef string, tail int) ([]string, error) {
    // 1. Получить содержимое буфера tmux
    cmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "capture-pane",
        "-t", runtimeRef,
        "-p",
    )
    
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("failed to capture pane: %w", err)
    }
    
    // 2. Разбить на строки
    lines := strings.Split(string(output), "\n")
    
    // 3. Вернуть последние N строк
    if len(lines) > tail {
        lines = lines[len(lines)-tail:]
    }
    
    return lines, nil
}
```


______________________________________________________________________

## Session Manager

**Управление адаптерами:**

```go
package session

import (
    "context"
    "fmt"
    "sync"
    
    "daemon/internal/runtime"
)

type SessionManager struct {
    adapters map[runtime.Type]runtime.RuntimeAdapter
    repo     SessionRepository
    mu       sync.RWMutex
}

type ManagerConfig struct {
    Container   runtime.ContainerAdapterConfig
    Process     runtime.ProcessAdapterConfig
    TMUX        runtime.TMUXAdapterConfig
    DefaultType runtime.Type
}

func NewSessionManager(cfg ManagerConfig, repo SessionRepository) (*SessionManager, error) {
    adapters := make(map[runtime.Type]runtime.RuntimeAdapter)
    
    // Container adapter
    if cfg.Container.DockerSocket != "" {
        containerAdapter, err := runtime.NewContainerAdapter(cfg.Container)
        if err != nil {
            return nil, fmt.Errorf("failed to create container adapter: %w", err)
        }
        adapters[runtime.TypeContainer] = containerAdapter
    }
    
    // Process adapter
    processAdapter := runtime.NewProcessAdapter(cfg.Process)
    adapters[runtime.TypeProcess] = processAdapter
    
    // TMUX adapter
    tmuxAdapter := runtime.NewTMUXAdapter(cfg.TMUX)
    adapters[runtime.TypeTMUX] = tmuxAdapter
    
    return &SessionManager{
        adapters: adapters,
        repo:     repo,
    }, nil
}
```

**Запуск сессии:**

```go
func (m *SessionManager) StartSession(ctx context.Context, req StartSessionRequest) (*Session, error) {
    // 1. Получить адаптер
    adapter, ok := m.adapters[req.RuntimeType]
    if !ok {
        return nil, fmt.Errorf("unknown runtime type: %s", req.RuntimeType)
    }
    
    // 2. Создать запись в БД
    session := &Session{
        TeamID:      req.TeamID,
        RoleID:      req.RoleID,
        QueueTaskID: req.QueueTaskID,
        RuntimeType: req.RuntimeType,
        State:       runtime.StateStarting,
        WorkingDir:  req.WorkingDir,
        TargetDir:   req.TargetDir,
        Config:      req.Config,
    }
    
    if err := m.repo.Create(ctx, session); err != nil {
        return nil, err
    }
    
    // 3. Подготовить конфигурацию для адаптера
    adapterCfg := runtime.SessionConfig{
        SessionID:   session.ID,
        TaskID:      req.QueueTaskID,
        Role:        req.Role,
        AgentType:   req.AgentType,
        WorkingDir:  req.WorkingDir,
        TargetDir:   req.TargetDir,
        AgentSpec:   req.AgentSpec,
        Profile:     req.Profile,
        Environment: req.Environment,
        CPU:         req.CPU,
        Memory:      req.Memory,
        Timeout:     req.Timeout,
    }
    
    // 4. Запустить сессию через адаптер
    runtimeRef, err := adapter.Start(ctx, adapterCfg)
    if err != nil {
        session.State = runtime.StateFailed
        m.repo.Update(ctx, session)
        return nil, fmt.Errorf("failed to start session: %w", err)
    }
    
    // 5. Обновить запись
    session.RuntimeRef = runtimeRef
    session.State = runtime.StateRunning
    session.StartedAt = time.Now()
    
    if err := m.repo.Update(ctx, session); err != nil {
        return nil, err
    }
    
    return session, nil
}
```

**Остановка сессии:**

```go
func (m *SessionManager) StopSession(ctx context.Context, sessionID int64, reason string) error {
    // 1. Получить сессию
    session, err := m.repo.GetByID(ctx, sessionID)
    if err != nil {
        return err
    }
    
    // 2. Получить адаптер
    adapter, ok := m.adapters[session.RuntimeType]
    if !ok {
        return fmt.Errorf("unknown runtime type: %s", session.RuntimeType)
    }
    
    // 3. Остановить сессию
    session.State = runtime.StateStopping
    m.repo.Update(ctx, session)
    
    if err := adapter.Stop(ctx, session.RuntimeRef); err != nil {
        session.State = runtime.StateFailed
        m.repo.Update(ctx, session)
        return err
    }
    
    // 4. Обновить статус
    session.State = runtime.StateStopped
    session.StoppedAt = time.Now()
    m.repo.Update(ctx, session)
    
    return nil
}
```

**Мониторинг сессий:**

```go
func (m *SessionManager) MonitorSessions(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            m.checkSessions(ctx)
        }
    }
}

func (m *SessionManager) checkSessions(ctx context.Context) {
    // 1. Получить все running сессии
    sessions, err := m.repo.ListByState(ctx, runtime.StateRunning)
    if err != nil {
        log.Error("failed to list sessions", "err", err)
        return
    }
    
    // 2. Проверить каждую сессию
    for _, session := range sessions {
        adapter, ok := m.adapters[session.RuntimeType]
        if !ok {
            continue
        }
        
        state, err := adapter.Status(ctx, session.RuntimeRef)
        if err != nil {
            log.Error("failed to get session status", "session_id", session.ID, "err", err)
            continue
        }
        
        // 3. Если статус изменился, обновить БД
        if state != session.State {
            session.State = state
            m.repo.Update(ctx, session)
            
            if state == runtime.StateStopped || state == runtime.StateFailed {
                log.Info("session stopped", "session_id", session.ID, "state", state)
            }
        }
    }
}
```


______________________________________________________________________

## Пример использования

**Запуск Lead-сессии:**

```go
// В Task Orchestrator
func (o *TaskOrchestrator) startLeadSession(ctx context.Context, task *Task) error {
    // 1. Найти Lead роль
    leadRole, err := o.roleRepo.FindByTeamAndName(ctx, task.TeamID, "lead")
    if err != nil {
        return err
    }
    
    // 2. Запустить сессию
    session, err := o.sessionManager.StartSession(ctx, StartSessionRequest{
        TeamID:      task.TeamID,
        RoleID:      leadRole.ID,
        QueueTaskID: task.ID,
        RuntimeType: "container",  // или "process", "tmux"
        Role:        "lead",
        AgentType:   "llm_coordinator",
        WorkingDir:  "/workspace",
        TargetDir:   "/projects/my-project",
        AgentSpec:   "agents/lead-coordinator/agent.yaml",
        Profile:     "default",
        CPU:         "1.0",
        Memory:      "512M",
    })
    
    if err != nil {
        return err
    }
    
    // 3. Обновить задачу
    task.SessionID = session.ID
    task.State = StateInProgress
    
    return o.taskRepo.Update(ctx, task)
}
```


______________________________________________________________________

## Рекомендации

### 1. **Container** — для production

- Изоляция, безопасность, воспроизводимость
- Легко масштабировать
- GPU поддержка для локальных LLM


### 2. **Process** — для разработки/тестов

- Быстрее, нет оверхеда Docker
- Проще отладка
- Меньше ресурсов


### 3. **TMUX** — для интерактивной работы

- Можно подключиться к сессии
- Видно, что делает агент в реальном времени
- Хорошо для debugging
