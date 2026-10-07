
# Безопасность — isolation, permissions, audit

## Архитектура безопасности

```
┌──────────────────────────────────────────────────────────┐
│                    SECURITY LAYERS                       │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  1. ISOLATION                                      │ │
│  │     - Контейнеры / namespaces                     │ │
│  │     - Отдельные пользователи                      │ │
│  │     - Filesystem isolation                        │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  2. PERMISSIONS                                    │ │
│  │     - RBAC (роли и права)                         │ │
│  │     - Capabilities (Linux)                        │ │
│  │     - Filesystem permissions                      │ │
│  │     - Network policies                            │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  3. AUDIT                                          │ │
│  │     - Логирование всех действий                   │ │
│  │     - Audit trail в БД                            │ │
│  │     - Security events                             │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  4. SECRETS                                        │ │
│  │     - Vault / secrets manager                     │ │
│  │     - Encryption at rest                          │ │
│  │     - Rotation                                    │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
└──────────────────────────────────────────────────────────┘
```


______________________________________________________________________

## 1. ISOLATION

### 1.1 Container Isolation (Docker)

**Безопасная конфигурация контейнеров:**

```go
package runtime

import (
    "github.com/docker/docker/api/types/container"
    "github.com/docker/docker/api/types/mount"
    "github.com/docker/docker/api/types/strslice"
)

func (a *ContainerAdapter) createSecureContainerConfig(cfg SessionConfig) *container.Config {
    return &container.Config{
        Image:      a.config.Image,
        Cmd:        a.buildCommand(cfg),
        Env:        a.buildEnvironment(cfg),
        WorkingDir: cfg.WorkingDir,
        
        // Security
        User: "1000:1000",  // non-root пользователь
        Labels: map[string]string{
            "daemon.session_id": fmt.Sprintf("%d", cfg.SessionID),
            "security.isolated": "true",
        },
        
        // Network
        AttachStdin:  false,  // отключить интерактивный stdin
        AttachStdout: true,
        AttachStderr: true,
        Tty:          false,  // отключить TTY для безопасности
        OpenStdin:    false,
    }
}

func (a *ContainerAdapter) createSecureHostConfig(cfg SessionConfig) *container.HostConfig {
    return &container.HostConfig{
        AutoRemove:  true,
        NetworkMode: container.NetworkMode(a.config.Network),
        
        // Security: отключить привилегии
        Privileged: false,
        
        // Security: ограничить capabilities
        CapDrop: []string{
            "ALL",  // drop all capabilities
        },
        CapAdd: []string{
            // Добавить только необходимые
            // "CHOWN",  // если нужно менять владельца файлов
        },
        
        // Security: read-only root filesystem
        ReadonlyRootfs: true,
        
        // Временные директории (writable)
        Tmpfs: map[string]string{
            "/tmp": "rw,noexec,nosuid,size=100M",
        },
        
        // Mounts (только необходимые, read-only где возможно)
        Mounts: []mount.Mount{
            {
                Type:   mount.TypeBind,
                Source: cfg.TargetDir,
                Target: cfg.TargetDir,
                ReadOnly: false,  // нужно для записи
                BindOptions: &mount.BindOptions{
                    NonRecursive: true,
                },
            },
            {
                Type:   mount.TypeTmpfs,
                Target: "/home",
                TmpfsOptions: &mount.TmpfsOptions{
                    SizeBytes: 50 * 1024 * 1024,  // 50MB
                    Mode:      0755,
                },
            },
        },
        
        // Security: ограничить ресурсы
        Resources: container.Resources{
            Memory:   a.parseMemory(cfg.Memory),
            NanoCPUs: a.parseCPU(cfg.CPU),
            
            // Ограничить PIDs (защита от fork bomb)
            PidsLimit: 100,
            
            // Ограничить количество файлов
            Ulimits: []*units.Ulimit{
                {
                    Name: "nofile",
                    Soft: 1024,
                    Hard: 2048,
                },
                {
                    Name: "nproc",
                    Soft: 50,
                    Hard: 100,
                },
            },
        },
        
        // Security: отключить network где не нужно
        PublishAllPorts: false,
        PortBindings:    nat.PortMap{},  // никаких опубликованных портов
        
        // Security: отключить IPC
        IpcMode: container.IPCMode("none"),
        
        // Security: отключить sharing с хостом
        PidMode:    container.PidMode(""),
        UsernsMode: container.UsernsMode("host"),  // использовать user namespace
        
        // Security: seccomp profile
        SecurityOpt: []string{
            "seccomp:unconfined",  // или свой профиль
        },
        
        // AppArmor (если включен)
        ApparmorProfile: "docker-default",
    }
}
```

**Custom seccomp профиль:**

```json
// seccomp-profile.json
{
  "defaultAction": "SCMP_ACT_ERRNO",
  "archMap": [
    {
      "architecture": "amd64",
      "subArchitectures": ["x86_64"]
    }
  ],
  "syscalls": [
    {
      "names": [
        "accept",
        "access",
        "bind",
        "connect",
        "execve",
        "exit",
        "exit_group",
        "getcwd",
        "getpid",
        "getuid",
        "listen",
        "open",
        "read",
        "socket",
        "stat",
        "write"
      ],
      "action": "SCMP_ACT_ALLOW"
    }
  ]
}
```

**Применение seccomp:**

```go
HostConfig: &container.HostConfig{
    SecurityOpt: []string{
        "seccomp=/path/to/seccomp-profile.json",
    },
}
```


______________________________________________________________________

### 1.2 User Isolation

**Запуск от разных пользователей:**

```go
package security

import (
    "fmt"
    "os/exec"
    "strconv"
)

// CreateUserForSession создаёт изолированного пользователя для сессии
func CreateUserForSession(sessionID int64) (uid, gid int, err error) {
    username := fmt.Sprintf("agent-%d", sessionID)
    
    // Создать группу
    gidCmd := exec.Command("groupadd", "-r", username)
    if err := gidCmd.Run(); err != nil {
        return 0, 0, err
    }
    
    // Создать пользователя
    uidCmd := exec.Command(
        "useradd",
        "-r",           // system user
        "-g", username, // primary group
        "-s", "/bin/false",  // no login shell
        "-M",           // no home directory
        username,
    )
    
    if err := uidCmd.Run(); err != nil {
        return 0, 0, err
    }
    
    // Получить UID/GID
    uidStr, _ := exec.Command("id", "-u", username).Output()
    gidStr, _ := exec.Command("id", "-g", username).Output()
    
    uid, _ = strconv.Atoi(string(uidStr))
    gid, _ = strconv.Atoi(string(gidStr))
    
    return uid, gid, nil
}

// DeleteUserForSession удаляет пользователя после завершения сессии
func DeleteUserForSession(sessionID int64) error {
    username := fmt.Sprintf("agent-%d", sessionID)
    
    // Удалить пользователя
    if err := exec.Command("userdel", username).Run(); err != nil {
        return err
    }
    
    // Удалить группу
    return exec.Command("groupdel", username).Run()
}
```

**Использование в ContainerAdapter:**

```go
func (a *ContainerAdapter) Start(ctx context.Context, cfg SessionConfig) (string, error) {
    // Создать изолированного пользователя
    uid, gid, err := CreateUserForSession(cfg.SessionID)
    if err != nil {
        return "", err
    }
    
    // Установить в конфиг контейнера
    containerCfg.User = fmt.Sprintf("%d:%d", uid, gid)
    
    // ... остальная логика запуска
}
```


______________________________________________________________________

### 1.3 Filesystem Isolation

**Ограничение доступа к файлам:**

```go
package security

import (
    "os"
    "path/filepath"
    "strings"
)

// FilesystemPolicy определяет правила доступа к файлам
type FilesystemPolicy struct {
    AllowedPaths    []string  // разрешённые пути
    ForbiddenPaths  []string  // запрещённые пути
    ReadOnlyPaths   []string  // только чтение
    MaxDepth        int       // максимальная глубина вложенности
}

// CanAccess проверяет, можно ли получить доступ к пути
func (p *FilesystemPolicy) CanAccess(path string, write bool) (bool, error) {
    // Нормализовать путь
    absPath, err := filepath.Abs(path)
    if err != nil {
        return false, err
    }
    
    // Проверить запрещённые пути
    for _, forbidden := range p.ForbiddenPaths {
        if strings.HasPrefix(absPath, forbidden) {
            return false, fmt.Errorf("access to %s is forbidden", path)
        }
    }
    
    // Проверить разрешённые пути
    allowed := false
    for _, allowedPath := range p.AllowedPaths {
        if strings.HasPrefix(absPath, allowedPath) {
            allowed = true
            break
        }
    }
    
    if !allowed {
        return false, fmt.Errorf("access to %s is not allowed", path)
    }
    
    // Проверить read-only
    if write {
        for _, roPath := range p.ReadOnlyPaths {
            if strings.HasPrefix(absPath, roPath) {
                return false, fmt.Errorf("path %s is read-only", path)
            }
        }
    }
    
    // Проверить глубину
    depth := strings.Count(absPath, string(os.PathSeparator))
    if depth > p.MaxDepth {
        return false, fmt.Errorf("path depth exceeds limit")
    }
    
    return true, nil
}

// DefaultPolicy для Pi-агентов
func DefaultFilesystemPolicy(workspaceDir string) *FilesystemPolicy {
    return &FilesystemPolicy{
        AllowedPaths: []string{
            workspaceDir,
            "/tmp",
        },
        ForbiddenPaths: []string{
            "/etc",
            "/root",
            "/home",
            "/var/log",
            "/proc",
            "/sys",
        },
        ReadOnlyPaths: []string{
            filepath.Join(workspaceDir, "vendor"),
            filepath.Join(workspaceDir, "node_modules"),
        },
        MaxDepth: 50,
    }
}
```


______________________________________________________________________

## 2. PERMISSIONS

### 2.1 RBAC (Role-Based Access Control)

**Модель прав в БД:**

```sql
-- Роли (не путать с agent roles)
CREATE TABLE security_roles (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,  -- "admin", "operator", "viewer"
    description TEXT
);

-- Permissions
CREATE TABLE permissions (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,  -- "tasks.create", "sessions.stop", "config.update"
    description TEXT
);

-- Mapping ролей к permissions
CREATE TABLE role_permissions (
    role_id       BIGINT NOT NULL REFERENCES security_roles(id),
    permission_id BIGINT NOT NULL REFERENCES permissions(id),
    
    PRIMARY KEY (role_id, permission_id)
);

-- Пользователи (люди)
CREATE TABLE users (
    id          BIGSERIAL PRIMARY KEY,
    username    TEXT NOT NULL UNIQUE,
    email       TEXT,
    password_hash TEXT,
    role_id     BIGINT REFERENCES security_roles(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- API ключи для сервисов
CREATE TABLE api_keys (
    id          BIGSERIAL PRIMARY KEY,
    key         TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    user_id     BIGINT REFERENCES users(id),
    permissions JSONB,  -- дополнительные permissions
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Audit log
CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_id     BIGINT REFERENCES users(id),
    api_key_id  BIGINT REFERENCES api_keys(id),
    action      TEXT NOT NULL,  -- "task.create", "session.stop"
    resource    TEXT,           -- "task:123", "session:456"
    details     JSONB,
    ip_address  INET,
    user_agent  TEXT
);

-- Индексы для audit
CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp DESC);
CREATE INDEX idx_audit_log_user ON audit_log(user_id, timestamp DESC);
CREATE INDEX idx_audit_log_action ON audit_log(action, timestamp DESC);
```

**Go модели:**

```go
package security

type Permission string

const (
    PermTasksCreate    Permission = "tasks.create"
    PermTasksRead      Permission = "tasks.read"
    PermTasksUpdate    Permission = "tasks.update"
    PermTasksDelete    Permission = "tasks.delete"
    
    PermSessionsCreate Permission = "sessions.create"
    PermSessionsRead   Permission = "sessions.read"
    PermSessionsStop   Permission = "sessions.stop"
    
    PermConfigUpdate   Permission = "config.update"
    PermConfigRead     Permission = "config.read"
    
    PermAuditRead      Permission = "audit.read"
)

type Role struct {
    ID          int64
    Name        string
    Description string
    Permissions []Permission
}

type User struct {
    ID       int64
    Username string
    Email    string
    Role     *Role
}

type AuthContext struct {
    UserID    int64
    APIKeyID  *int64
    Permissions []Permission
}
```

**Middleware для проверки прав:**

```go
package api

import (
    "context"
    "net/http"
    "strings"
    
    "daemon/internal/security"
)

type contextKey string

const authContextKey = contextKey("auth")

// AuthMiddleware проверяет API ключ и добавляет контекст
func AuthMiddleware(authService *security.AuthService) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Получить API ключ из заголовка
            apiKey := r.Header.Get("X-API-Key")
            
            if apiKey == "" {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            
            // Проверить ключ
            authCtx, err := authService.ValidateAPIKey(r.Context(), apiKey)
            if err != nil {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            
            // Добавить в контекст
            ctx := context.WithValue(r.Context(), authContextKey, authCtx)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// RequirePermission middleware для проверки конкретного permission
func RequirePermission(permission security.Permission) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            authCtx, ok := r.Context().Value(authContextKey).(*security.AuthContext)
            if !ok {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            
            // Проверить permission
            if !hasPermission(authCtx.Permissions, permission) {
                http.Error(w, "Forbidden", http.StatusForbidden)
                return
            }
            
            next.ServeHTTP(w, r)
        })
    }
}

func hasPermission(permissions []security.Permission, required security.Permission) bool {
    for _, p := range permissions {
        if p == required {
            return true
        }
    }
    return false
}
```

**Использование в handlers:**

```go
func NewTaskHandler(taskService service.TaskService, authService *security.AuthService) *TaskHandler {
    mux := chi.NewMux()
    
    // Все endpoints требуют аутентификации
    mux.Use(AuthMiddleware(authService))
    
    // Создать задачу: требуется permission
    mux.Post("/", RequirePermission(security.PermTasksCreate)(
        http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // ... логика создания
        }),
    ))
    
    // Получить задачу: требуется permission
    mux.Get("/{id}", RequirePermission(security.PermTasksRead)(
        http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // ... логика получения
        }),
    ))
    
    // Остановить сессию: требуется permission
    mux.Post("/{id}/stop", RequirePermission(security.PermSessionsStop)(
        http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // ... логика остановки
        }),
    ))
    
    return &TaskHandler{mux: mux}
}
```


______________________________________________________________________

### 2.2 Linux Capabilities

**Ограничение capabilities для процессов:**

```go
package security

import (
    "syscall"
    
    "golang.org/x/sys/unix"
)

// DropAllCapabilities удаляет все capabilities у текущего процесса
func DropAllCapabilities() error {
    // Drop all capabilities
    for cap := 0; cap <= int(unix.CAP_LAST_CAP); cap++ {
        if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(cap), 0, 0, 0); err != nil {
            return err
        }
    }
    
    return nil
}

// SetNoNewPrivs устанавливает flag no_new_privs
func SetNoNewPrivs() error {
    return unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0)
}

// ApplySecurityProfile применяет профиль безопасности
func ApplySecurityProfile() error {
    // Удалить все capabilities
    if err := DropAllCapabilities(); err != nil {
        return err
    }
    
    // Установить no_new_privs
    if err := SetNoNewPrivs(); err != nil {
        return err
    }
    
    return nil
}
```

**В ProcessAdapter:**

```go
func (a *ProcessAdapter) Start(ctx context.Context, cfg SessionConfig) (string, error) {
    cmd := exec.Command(a.config.RuntimeBinary, args...)
    
    // Установить UID/GID
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Cred: &syscall.Credential{
            Uid: 1000,  // non-root
            Gid: 1000,
        },
    }
    
    // Ограничить capabilities
    if err := ApplySecurityProfile(); err != nil {
        log.Warn("failed to apply security profile", "err", err)
    }
    
    // ... остальная логика
}
```


______________________________________________________________________

## 3. AUDIT

### 3.1 Audit Logger

**Сервис для логирования:**

```go
package security

import (
    "context"
    "encoding/json"
    "time"
    
    "github.com/jackc/pgx/v5"
)

type AuditLogger struct {
    db *pgx.Conn
}

type AuditEvent struct {
    UserID     *int64
    APIKeyID   *int64
    Action     string
    Resource   string
    Details    map[string]interface{}
    IPAddress  string
    UserAgent  string
    Timestamp  time.Time
}

func NewAuditLogger(db *pgx.Conn) *AuditLogger {
    return &AuditLogger{db: db}
}

func (l *AuditLogger) Log(ctx context.Context, event AuditEvent) error {
    query := `
        INSERT INTO audit_log (
            user_id, api_key_id, action, resource, 
            details, ip_address, user_agent, timestamp
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8
        )
    `
    
    detailsJSON, err := json.Marshal(event.Details)
    if err != nil {
        return err
    }
    
    _, err = l.db.Exec(ctx, query,
        event.UserID,
        event.APIKeyID,
        event.Action,
        event.Resource,
        detailsJSON,
        event.IPAddress,
        event.UserAgent,
        event.Timestamp,
    )
    
    return err
}

// LogTaskCreate логирует создание задачи
func (l *AuditLogger) LogTaskCreate(ctx context.Context, userID *int64, taskID int64, details map[string]interface{}) error {
    return l.Log(ctx, AuditEvent{
        UserID:    userID,
        Action:    "task.create",
        Resource:  fmt.Sprintf("task:%d", taskID),
        Details:   details,
        Timestamp: time.Now(),
    })
}

// LogSessionStop логирует остановку сессии
func (l *AuditLogger) LogSessionStop(ctx context.Context, userID *int64, sessionID int64, reason string) error {
    return l.Log(ctx, AuditEvent{
        UserID:   userID,
        Action:   "session.stop",
        Resource: fmt.Sprintf("session:%d", sessionID),
        Details: map[string]interface{}{
            "reason": reason,
        },
        Timestamp: time.Now(),
    })
}

// LogConfigUpdate логирует изменение конфигурации
func (l *AuditLogger) LogConfigUpdate(ctx context.Context, userID *int64, sessionID int64, changes map[string]interface{}) error {
    return l.Log(ctx, AuditEvent{
        UserID:   userID,
        Action:   "config.update",
        Resource: fmt.Sprintf("session:%d", sessionID),
        Details: map[string]interface{}{
            "changes": changes,
        },
        Timestamp: time.Now(),
    })
}

// LogSecurityEvent логирует security события
func (l *AuditLogger) LogSecurityEvent(ctx context.Context, action string, details map[string]interface{}) error {
    return l.Log(ctx, AuditEvent{
        Action:    "security." + action,
        Details:   details,
        Timestamp: time.Now(),
    })
}
```

**Использование в handlers:**

```go
func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    // Получить auth контекст
    authCtx := ctx.Value(authContextKey).(*security.AuthContext)
    
    // ... логика создания задачи
    
    task, err := h.taskService.CreateTask(ctx, req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Audit log
    if err := h.auditLogger.LogTaskCreate(
        ctx,
        authCtx.UserID,
        task.ID,
        map[string]interface{}{
            "title":       req.Title,
            "project_id":  req.ProjectID,
            "role":        req.Role,
        },
    ); err != nil {
        log.Error("failed to log audit event", "err", err)
    }
    
    // ... ответ
}
```


______________________________________________________________________

### 3.2 Security Events

**Детекция подозрительной активности:**

```go
package security

type SecurityMonitor struct {
    auditLogger *AuditLogger
    alertChan   chan SecurityAlert
}

type SecurityAlert struct {
    Type      string
    Severity  string  // "low", "medium", "high", "critical"
    Message   string
    Details   map[string]interface{}
    Timestamp time.Time
}

func NewSecurityMonitor(auditLogger *AuditLogger) *SecurityMonitor {
    monitor := &SecurityMonitor{
        auditLogger: auditLogger,
        alertChan:   make(chan SecurityAlert, 100),
    }
    
    // Запустить обработчик алертов
    go monitor.processAlerts()
    
    return monitor
}

func (m *SecurityMonitor) CheckEvent(event AuditEvent) {
    // Проверить на подозрительную активность
    
    // 1. Множественные failed авторизации
    if event.Action == "auth.failed" {
        // Проверить количество failed попыток
        count, err := m.countFailedAuths(event.UserID, event.IPAddress, 5*time.Minute)
        if err != nil {
            return
        }
        
        if count > 10 {
            m.sendAlert(SecurityAlert{
                Type:     "brute_force",
                Severity: "high",
                Message:  "Multiple failed authentication attempts",
                Details: map[string]interface{}{
                    "user_id":    event.UserID,
                    "ip_address": event.IPAddress,
                    "count":      count,
                },
            })
        }
    }
    
    // 2. Попытка доступа без прав
    if event.Action == "permission.denied" {
        m.sendAlert(SecurityAlert{
            Type:     "unauthorized_access",
            Severity: "medium",
            Message:  "Unauthorized access attempt",
            Details: map[string]interface{}{
                "user_id":  event.UserID,
                "resource": event.Resource,
            },
        })
    }
    
    // 3. Подозрительные изменения конфига
    if event.Action == "config.update" {
        // Проверить, что изменилось
        if changes, ok := event.Details["changes"].(map[string]interface{}); ok {
            if _, hasPlugins := changes["plugins"]; hasPlugins {
                m.sendAlert(SecurityAlert{
                    Type:     "config_change",
                    Severity: "low",
                    Message:  "Plugin configuration changed",
                    Details:  event.Details,
                })
            }
        }
    }
}

func (m *SecurityMonitor) sendAlert(alert SecurityAlert) {
    alert.Timestamp = time.Now()
    m.alertChan <- alert
}

func (m *SecurityMonitor) processAlerts() {
    for alert := range m.alertChan {
        // Логировать алерт
        log.Warn("security alert",
            "type", alert.Type,
            "severity", alert.Severity,
            "message", alert.Message,
        )
        
        // Отправить в Slack/PagerDuty (опционально)
        if alert.Severity == "high" || alert.Severity == "critical" {
            m.sendToSlack(alert)
        }
        
        // Сохранить в БД
        m.saveAlert(alert)
    }
}
```


______________________________________________________________________

### 3.3 Audit Dashboard

**API для получения audit логов:**

```go
type AuditHandler struct {
    auditRepo AuditRepository
}

type ListAuditLogsRequest struct {
    UserID      *int64
    Action      string
    Resource    string
    StartTime   time.Time
    EndTime     time.Time
    Limit       int
    Offset      int
}

func (h *AuditHandler) ListLogs(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    // Получить параметры
    req := ListAuditLogsRequest{
        Limit: 100,
    }
    
    // ... парсинг query параметров
    
    // Проверить permission
    authCtx := ctx.Value(authContextKey).(*security.AuthContext)
    if !hasPermission(authCtx.Permissions, security.PermAuditRead) {
        http.Error(w, "Forbidden", http.StatusForbidden)
        return
    }
    
    // Получить логи
    logs, total, err := h.auditRepo.List(ctx, req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    // Вернуть ответ
    json.NewEncoder(w).Encode(map[string]interface{}{
        "logs":  logs,
        "total": total,
    })
}
```


______________________________________________________________________

## 4. SECRETS

### 4.1 Secrets Manager

**Хранение секретов:**

```go
package security

import (
    "context"
    "encoding/base64"
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "io"
)

type SecretsManager struct {
    encryptionKey []byte
    db            *pgx.Conn
}

type Secret struct {
    ID          int64
    Name        string
    Value       string  // encrypted
    Description string
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

func (m *SecretsManager) Store(ctx context.Context, name, value, description string) error {
    // Зашифровать значение
    encrypted, err := m.encrypt(value)
    if err != nil {
        return err
    }
    
    query := `
        INSERT INTO secrets (name, value, description)
        VALUES ($1, $2, $3)
        ON CONFLICT (name) DO UPDATE
        SET value = $2, description = $3, updated_at = NOW()
    `
    
    _, err = m.db.Exec(ctx, query, name, encrypted, description)
    return err
}

func (m *SecretsManager) Get(ctx context.Context, name string) (string, error) {
    var encrypted string
    
    query := `SELECT value FROM secrets WHERE name = $1`
    
    err := m.db.QueryRow(ctx, query, name).Scan(&encrypted)
    if err != nil {
        return "", err
    }
    
    // Расшифровать
    return m.decrypt(encrypted)
}

func (m *SecretsManager) encrypt(plaintext string) (string, error) {
    block, err := aes.NewCipher(m.encryptionKey)
    if err != nil {
        return "", err
    }
    
    ciphertext := make([]byte, aes.BlockSize+len(plaintext))
    iv := ciphertext[:aes.BlockSize]
    
    if _, err := io.ReadFull(rand.Reader, iv); err != nil {
        return "", err
    }
    
    stream := cipher.NewCFBEncrypter(block, iv)
    stream.XORKeyStream(ciphertext[aes.BlockSize:], []byte(plaintext))
    
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (m *SecretsManager) decrypt(ciphertextBase64 string) (string, error) {
    ciphertext, err := base64.StdEncoding.DecodeString(ciphertextBase64)
    if err != nil {
        return "", err
    }
    
    block, err := aes.NewCipher(m.encryptionKey)
    if err != nil {
        return "", err
    }
    
    if len(ciphertext) < aes.BlockSize {
        return "", fmt.Errorf("ciphertext too short")
    }
    
    iv := ciphertext[:aes.BlockSize]
    ciphertext = ciphertext[aes.BlockSize:]
    
    stream := cipher.NewCFBDecrypter(block, iv)
    stream.XORKeyStream(ciphertext, ciphertext)
    
    return string(ciphertext), nil
}
```

**Таблица secrets:**

```sql
CREATE TABLE secrets (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    value       TEXT NOT NULL,  -- encrypted
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индекс для поиска
CREATE INDEX idx_secrets_name ON secrets(name);
```


______________________________________________________________________

## Рекомендации

### 1. **Principle of Least Privilege**

- Минимальные права для каждого компонента
- Drop all capabilities, добавить только необходимые
- Non-root пользователи везде


### 2. **Defense in Depth**

- Multiple layers: isolation + permissions + audit
- Не полагаться на одну защиту


### 3. **Audit Everything**

- Все действия логировать
- Security events мониторить
- Регулярно ревьювить логи


### 4. **Secrets Management**

- Никогда не хранить в коде
- Шифрование at rest
- Rotation ключей


### 5. **Regular Security Reviews**

- Аудит конфигов
- Проверка dependencies
- Penetration testing

