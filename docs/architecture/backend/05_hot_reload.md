
# Hot reload — как обновлять конфиг без перезапуска

## Архитектура hot reload

```
┌──────────────────────────────────────────────────────────┐
│                     DAEMON                               │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Config Watcher                                    │ │
│  │                                                    │ │
│  │  1. Следит за изменениями в конфиге              │ │
│  │  2. Валидирует новый конфиг                       │ │
│  │  3. Отправляет сигнал Pi агенту                  │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
                          │
                          │ Signal (SIGHUP / custom)
                          │ или HTTP API
                          ▼
┌──────────────────────────────────────────────────────────┐
│              PI AGENT (в tmux сессии)                    │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Config Manager                                    │ │
│  │                                                    │ │
│  │  1. Получает сигнал о изменении                  │ │
│  │  2. Перечитывает конфиг                           │ │
│  │  3. Валидирует                                    │ │
│  │  4. Применяет изменения                           │ │
│  │     - плагины: включить/выключить                │ │
│  │     - модель: переключить                        │ │
│  │     - skills: загрузить новые                    │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────┘
```


______________________________________________________________________

## 1. Signal-based reload (SIGHUP)

**Daemon отправляет сигнал процессу Pi:**

```go
package runtime

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "syscall"
    "time"
    
    "github.com/fsnotify/fsnotify"
    "gopkg.in/yaml.v3"
)

// HotReloadConfig - конфигурация для hot reload
type HotReloadConfig struct {
    Enabled          bool
    WatchConfigFile  bool          // следить за файлом конфига
    WatchInterval    time.Duration // интервал опроса
    ValidateOnReload bool          // валидировать перед применением
    GracefulTimeout  time.Duration // таймаут для graceful reload
}

// PiAdapter с поддержкой hot reload
type PiAdapter struct {
    config        PiAdapterConfig
    reloadConfig  HotReloadConfig
    configWatchers map[string]*fsnotify.Watcher  // sessionID -> watcher
    mu            sync.RWMutex
}

func NewPiAdapter(cfg PiAdapterConfig, reloadCfg HotReloadConfig) *PiAdapter {
    adapter := &PiAdapter{
        config:        cfg,
        reloadConfig:  reloadCfg,
        configWatchers: make(map[string]*fsnotify.Watcher),
    }
    
    // Запустить watcher если включено
    if reloadCfg.Enabled && reloadCfg.WatchConfigFile {
        go adapter.startConfigWatcher()
    }
    
    return adapter
}
```

**Отправка сигнала SIGHUP:**

```go
func (a *PiAdapter) ReloadConfig(ctx context.Context, runtimeRef string) error {
    sessionName := runtimeRef  // "pi-123"
    
    // 1. Найти PID процесса Pi в tmux сессии
    pid, err := a.findPiProcessPID(sessionName)
    if err != nil {
        return fmt.Errorf("failed to find Pi process: %w", err)
    }
    
    // 2. Отправить SIGHUP
    process, err := os.FindProcess(pid)
    if err != nil {
        return fmt.Errorf("failed to find process: %w", err)
    }
    
    // Отправить сигнал
    if err := process.Signal(syscall.SIGHUP); err != nil {
        return fmt.Errorf("failed to send SIGHUP: %w", err)
    }
    
    log.Info("sent SIGHUP to Pi process", "session", sessionName, "pid", pid)
    
    return nil
}

func (a *PiAdapter) findPiProcessPID(sessionName string) (int, error) {
    // 1. Получить список процессов в tmux сессии
    cmd := exec.Command(
        a.config.TMUXBinary,
        "-L", a.config.SocketName,
        "list-panes",
        "-t", sessionName,
        "-F", "#{pane_pid}",
    )
    
    output, err := cmd.Output()
    if err != nil {
        return 0, fmt.Errorf("failed to list panes: %w", err)
    }
    
    // 2. Распарсить PID
    pidStr := strings.TrimSpace(string(output))
    if pidStr == "" {
        return 0, fmt.Errorf("no process found in session")
    }
    
    pid, err := strconv.Atoi(pidStr)
    if err != nil {
        return 0, fmt.Errorf("invalid PID: %w", err)
    }
    
    return pid, nil
}
```

**Обновление конфига с валидацией:**

```go
func (a *PiAdapter) UpdateConfig(ctx context.Context, runtimeRef string, newConfig *PiConfig) error {
    sessionName := runtimeRef
    sessionID := extractSessionID(sessionName)
    
    // 1. Сохранить новый конфиг
    configPath := filepath.Join(a.config.ConfigsDir, fmt.Sprintf("session-%d.yaml", sessionID))
    
    // 2. Валидировать конфиг
    if a.reloadConfig.ValidateOnReload {
        if err := a.validateConfig(newConfig); err != nil {
            return fmt.Errorf("config validation failed: %w", err)
        }
    }
    
    // 3. Сохранить временный файл
    tmpPath := configPath + ".tmp"
    
    data, err := yaml.Marshal(newConfig)
    if err != nil {
        return err
    }
    
    if err := os.WriteFile(tmpPath, data, 0644); err != nil {
        return err
    }
    
    // 4. Атомарно заменить конфиг
    if err := os.Rename(tmpPath, configPath); err != nil {
        return err
    }
    
    // 5. Отправить сигнал на reload
    if err := a.ReloadConfig(ctx, sessionName); err != nil {
        // Откатить конфиг при ошибке
        log.Error("failed to reload config, rolling back", "err", err)
        // Можно восстановить из бэкапа
        return err
    }
    
    log.Info("config updated and reloaded", "session", sessionName)
    
    return nil
}

func (a *PiAdapter) validateConfig(cfg *PiConfig) error {
    // Проверить обязательные поля
    if cfg.Model == "" {
        return fmt.Errorf("model is required")
    }
    
    // Проверить модель (список доступных)
    validModels := []string{
        "claude-3-7-sonnet",
        "claude-3-5-sonnet",
        "gpt-4.1",
        "gpt-4-turbo",
        "local-llm",
    }
    
    valid := false
    for _, model := range validModels {
        if cfg.Model == model {
            valid = true
            break
        }
    }
    
    if !valid {
        return fmt.Errorf("invalid model: %s", cfg.Model)
    }
    
    // Проверить плагины
    for _, plugin := range cfg.Plugins {
        if err := a.validatePlugin(plugin); err != nil {
            return fmt.Errorf("invalid plugin %s: %w", plugin.Name, err)
        }
    }
    
    // Проверить MCP серверы
    for _, server := range cfg.MCP.Servers {
        if err := a.validateMCPServer(server); err != nil {
            return fmt.Errorf("invalid MCP server %s: %w", server.Name, err)
        }
    }
    
    return nil
}
```


______________________________________________________________________

## 2. Watcher за файлом конфига

**Автоматический reload при изменении файла:**

```go
func (a *PiAdapter) startConfigWatcher() {
    ticker := time.NewTicker(a.reloadConfig.WatchInterval)
    defer ticker.Stop()
    
    // Мапа для хранения последних модификаций
    lastMod := make(map[string]time.Time)
    
    for {
        select {
        case <-ticker.C:
            a.checkConfigChanges(lastMod)
        }
    }
}

func (a *PiAdapter) checkConfigChanges(lastMod map[string]time.Time) {
    // 1. Получить все активные Pi сессии
    sessions, err := a.getActiveSessions()
    if err != nil {
        log.Error("failed to get active sessions", "err", err)
        return
    }
    
    // 2. Проверить каждый конфиг
    for _, session := range sessions {
        configPath := filepath.Join(a.config.ConfigsDir, fmt.Sprintf("session-%d.yaml", session.ID))
        
        // Получить время модификации
        info, err := os.Stat(configPath)
        if err != nil {
            continue
        }
        
        modTime := info.ModTime()
        
        // Проверить, изменился ли файл
        if lastModTime, ok := lastMod[configPath]; ok {
            if modTime.After(lastModTime) {
                log.Info("config file changed, reloading", "session", session.ID)
                
                // Загрузить новый конфиг
                newConfig, err := a.loadPiConfig(configPath)
                if err != nil {
                    log.Error("failed to load new config", "err", err)
                    continue
                }
                
                // Валидировать
                if a.reloadConfig.ValidateOnReload {
                    if err := a.validateConfig(newConfig); err != nil {
                        log.Error("new config validation failed", "err", err)
                        continue
                    }
                }
                
                // Отправить сигнал на reload
                if err := a.ReloadConfig(context.Background(), fmt.Sprintf("pi-%d", session.ID)); err != nil {
                    log.Error("failed to reload config", "err", err)
                }
            }
        }
        
        lastMod[configPath] = modTime
    }
}
```

**Или через fsnotify (более эффективно):**

```go
func (a *PiAdapter) startFSNotifyWatcher() error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }
    
    // Следить за директорией с конфигами
    if err := watcher.Add(a.config.ConfigsDir); err != nil {
        return err
    }
    
    go func() {
        for {
            select {
            case event, ok := <-watcher.Events:
                if !ok {
                    return
                }
                
                // Проверить, что это изменение файла
                if event.Op&fsnotify.Write == fsnotify.Write {
                    sessionID := a.extractSessionIDFromPath(event.Name)
                    
                    log.Info("config file modified", "path", event.Name, "session", sessionID)
                    
                    // Небольшая задержка, чтобы файл полностью записался
                    time.Sleep(100 * time.Millisecond)
                    
                    // Загрузить и валидировать
                    newConfig, err := a.loadPiConfig(event.Name)
                    if err != nil {
                        log.Error("failed to load new config", "err", err)
                        continue
                    }
                    
                    if a.reloadConfig.ValidateOnReload {
                        if err := a.validateConfig(newConfig); err != nil {
                            log.Error("config validation failed", "err", err)
                            continue
                        }
                    }
                    
                    // Отправить сигнал
                    if err := a.ReloadConfig(context.Background(), fmt.Sprintf("pi-%d", sessionID)); err != nil {
                        log.Error("failed to reload config", "err", err)
                    }
                }
                
            case err, ok := <-watcher.Errors:
                if !ok {
                    return
                }
                log.Error("watcher error", "err", err)
            }
        }
    }()
    
    return nil
}
```


______________________________________________________________________

## 3. HTTP API для hot reload

**Endpoint для обновления конфига:**

```go
type UpdatePiConfigRequest struct {
    SessionID   int64       `json:"session_id"`
    Model       string      `json:"model,omitempty"`
    Temperature float64     `json:"temperature,omitempty"`
    Plugins     PluginUpdates `json:"plugins,omitempty"`
    Skills      []string    `json:"skills,omitempty"`
    MCP         *MCPUpdates `json:"mcp,omitempty"`
}

type PluginUpdates map[string]PluginUpdate

type PluginUpdate struct {
    Enabled *bool                  `json:"enabled,omitempty"`
    Config  map[string]interface{} `json:"config,omitempty"`
}

type MCPUpdates struct {
    Servers []MCPServerUpdate `json:"servers"`
}

type MCPServerUpdate struct {
    Name    string `json:"name"`
    Enabled *bool  `json:"enabled,omitempty"`
    URL     string `json:"url,omitempty"`
}

func (h *SessionHandler) UpdatePiConfig(w http.ResponseWriter, r *http.Request) {
    var req UpdatePiConfigRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    ctx := r.Context()
    
    // 1. Получить сессию
    session, err := h.sessionRepo.GetByID(ctx, req.SessionID)
    if err != nil {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }
    
    // 2. Проверить, что это Pi сессия
    if session.RuntimeType != "pi" {
        http.Error(w, "not a Pi session", http.StatusBadRequest)
        return
    }
    
    // 3. Загрузить текущий конфиг
    configPath := filepath.Join(h.piAdapter.config.ConfigsDir, fmt.Sprintf("session-%d.yaml", req.SessionID))
    
    currentConfig, err := h.piAdapter.loadPiConfig(configPath)
    if err != nil {
        http.Error(w, "failed to load config", http.StatusInternalServerError)
        return
    }
    
    // 4. Применить изменения
    newConfig := currentConfig
    
    if req.Model != "" {
        newConfig.Model = req.Model
    }
    
    if req.Temperature > 0 {
        newConfig.Temperature = req.Temperature
    }
    
    // Обновить плагины
    for name, update := range req.Plugins {
        for i, plugin := range newConfig.Plugins {
            if plugin.Name == name {
                if update.Enabled != nil {
                    newConfig.Plugins[i].Enabled = *update.Enabled
                }
                if update.Config != nil {
                    newConfig.Plugins[i].Config = update.Config
                }
            }
        }
    }
    
    // 5. Валидировать новый конфиг
    if err := h.piAdapter.validateConfig(newConfig); err != nil {
        http.Error(w, fmt.Sprintf("validation failed: %v", err), http.StatusBadRequest)
        return
    }
    
    // 6. Сохранить и применить
    if err := h.piAdapter.UpdateConfig(ctx, fmt.Sprintf("pi-%d", req.SessionID), newConfig); err != nil {
        http.Error(w, fmt.Sprintf("failed to update config: %v", err), http.StatusInternalServerError)
        return
    }
    
    // 7. Вернуть ответ
    json.NewEncoder(w).Encode(map[string]interface{}{
        "status":  "ok",
        "session": req.SessionID,
        "message": "config updated and reloaded",
    })
}
```

**Пример запроса:**

```bash
# Обновить модель и включить плагин
curl -X POST http://localhost:8080/api/v1/sessions/123/pi/config \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": 123,
    "model": "gpt-4.1",
    "temperature": 0.5,
    "plugins": {
      "github": {
        "enabled": true,
        "config": {
          "repo": "myorg/new-repo",
          "auto_create_pr": true
        }
      },
      "terminal": {
        "enabled": false
      }
    }
  }'
```


______________________________________________________________________

## 4. Graceful reload с откатом

**Сохранение бэкапа и откат при ошибке:**

```go
func (a *PiAdapter) UpdateConfigWithRollback(ctx context.Context, runtimeRef string, newConfig *PiConfig) error {
    sessionID := extractSessionID(runtimeRef)
    configPath := filepath.Join(a.config.ConfigsDir, fmt.Sprintf("session-%d.yaml", sessionID))
    backupPath := configPath + ".backup"
    
    // 1. Создать бэкап
    if err := a.createBackup(configPath, backupPath); err != nil {
        return fmt.Errorf("failed to create backup: %w", err)
    }
    
    // 2. Сохранить новый конфиг
    if err := a.savePiConfig(configPath, newConfig); err != nil {
        return err
    }
    
    // 3. Отправить сигнал на reload
    if err := a.ReloadConfig(ctx, runtimeRef); err != nil {
        log.Error("reload failed, rolling back", "err", err)
        
        // Откатить бэкап
        if rollbackErr := a.restoreBackup(backupPath, configPath); rollbackErr != nil {
            return fmt.Errorf("reload failed and rollback failed: %w", rollbackErr)
        }
        
        return fmt.Errorf("reload failed, rolled back: %w", err)
    }
    
    // 4. Подождать подтверждения успешного reload
    if err := a.waitForReloadConfirmation(ctx, runtimeRef, a.reloadConfig.GracefulTimeout); err != nil {
        log.Error("reload confirmation failed, rolling back", "err", err)
        
        // Откатить
        a.restoreBackup(backupPath, configPath)
        
        return fmt.Errorf("reload confirmation failed: %w", err)
    }
    
    // 5. Удалить бэкап (успех)
    os.Remove(backupPath)
    
    return nil
}

func (a *PiAdapter) createBackup(src, dst string) error {
    data, err := os.ReadFile(src)
    if err != nil {
        return err
    }
    
    return os.WriteFile(dst, data, 0644)
}

func (a *PiAdapter) restoreBackup(src, dst string) error {
    data, err := os.ReadFile(src)
    if err != nil {
        return err
    }
    
    return os.WriteFile(dst, data, 0644)
}

func (a *PiAdapter) waitForReloadConfirmation(ctx context.Context, runtimeRef string, timeout time.Duration) error {
    // 1. Подождать пока Pi перечитает конфиг
    time.Sleep(1 * time.Second)
    
    // 2. Проверить статус сессии
    deadline := time.Now().Add(timeout)
    
    for time.Now().Before(deadline) {
        status, err := a.Status(ctx, runtimeRef)
        if err != nil {
            return err
        }
        
        if status == StateRunning {
            // Проверить, что Pi отвечает (можно через health check endpoint)
            if err := a.checkPiHealth(runtimeRef); err == nil {
                return nil  // успех
            }
        }
        
        time.Sleep(500 * time.Millisecond)
    }
    
    return fmt.Errorf("timeout waiting for reload confirmation")
}

func (a *PiAdapter) checkPiHealth(runtimeRef string) error {
    sessionID := extractSessionID(runtimeRef)
    
    // Предположим, что Pi слушает HTTP для health checks
    url := fmt.Sprintf("http://localhost:%d/health", 9000+sessionID)
    
    client := &http.Client{Timeout: 2 * time.Second}
    resp, err := client.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return fmt.Errorf("health check failed: %d", resp.StatusCode)
    }
    
    return nil
}
```


______________________________________________________________________

## 5. Pi-агент: обработка SIGHUP

**Пример обработки сигнала в самом Pi:**

```go
// В коде Pi runtime
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"
)

func main() {
    // Загрузить начальный конфиг
    config := loadConfig(os.Getenv("PI_CONFIG"))
    
    // Инициализировать агент
    agent := NewAgent(config)
    
    // Настроить обработчик сигналов
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
    
    // Канал для reload
    reloadChan := make(chan *Config, 1)
    
    // Запустить goroutine для обработки сигналов
    go func() {
        for sig := range sigChan {
            switch sig {
            case syscall.SIGHUP:
                log.Println("Received SIGHUP, reloading config...")
                
                // Перечитать конфиг
                newConfig := loadConfig(os.Getenv("PI_CONFIG"))
                
                // Валидировать
                if err := validateConfig(newConfig); err != nil {
                    log.Printf("Config validation failed: %v", err)
                    continue
                }
                
                // Отправить в канал для применения
                reloadChan <- newConfig
                
            case syscall.SIGTERM, syscall.SIGINT:
                log.Println("Received shutdown signal")
                
                // Graceful shutdown
                ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
                defer cancel()
                
                if err := agent.Shutdown(ctx); err != nil {
                    log.Printf("Shutdown error: %v", err)
                }
                
                os.Exit(0)
            }
        }
    }()
    
    // Основной цикл
    for {
        select {
        case newConfig := <-reloadChan:
            log.Println("Applying new configuration...")
            
            // Graceful reload
            if err := agent.ReloadConfig(newConfig); err != nil {
                log.Printf("Reload failed: %v", err)
                // Продолжать со старым конфигом
            } else {
                log.Println("Configuration reloaded successfully")
            }
            
        default:
            // Основная работа агента
            agent.Run()
            time.Sleep(100 * time.Millisecond)
        }
    }
}
```

**Метод ReloadConfig в агенте:**

```go
func (a *Agent) ReloadConfig(newConfig *Config) error {
    a.mu.Lock()
    defer a.mu.Unlock()
    
    oldConfig := a.config
    
    // 1. Обновить модель (если изменилась)
    if newConfig.Model != oldConfig.Model {
        if err := a.switchModel(newConfig.Model); err != nil {
            return fmt.Errorf("failed to switch model: %w", err)
        }
    }
    
    // 2. Обновить плагины
    for _, plugin := range newConfig.Plugins {
        if plugin.Enabled {
            // Включить или обновить плагин
            if err := a.enablePlugin(plugin); err != nil {
                log.Printf("Failed to enable plugin %s: %v", plugin.Name, err)
            }
        } else {
            // Выключить плагин
            a.disablePlugin(plugin.Name)
        }
    }
    
    // 3. Обновить skills
    for _, skill := range newConfig.Skills {
        if skill.Enabled {
            if err := a.loadSkill(skill.Path); err != nil {
                log.Printf("Failed to load skill %s: %v", skill.Path, err)
            }
        }
    }
    
    // 4. Обновить MCP серверы
    a.updateMCPServers(newConfig.MCP)
    
    // 5. Применить новые настройки
    a.config = newConfig
    
    log.Println("Configuration reload complete")
    
    return nil
}

func (a *Agent) switchModel(model string) error {
    log.Printf("Switching model from %s to %s", a.config.Model, model)
    
    // 1. Закрыть старое соединение с LLM
    if a.llmClient != nil {
        a.llmClient.Close()
    }
    
    // 2. Создать новый клиент
    client, err := createLLMClient(model, a.config)
    if err != nil {
        return err
    }
    
    // 3. Заменить клиент
    a.llmClient = client
    
    log.Printf("Model switched to %s", model)
    
    return nil
}

func (a *Agent) enablePlugin(plugin PluginConfig) error {
    // Проверить, включён ли уже
    if _, exists := a.plugins[plugin.Name]; exists {
        // Обновить конфиг
        a.plugins[plugin.Name].UpdateConfig(plugin.Config)
        return nil
    }
    
    // Загрузить и включить плагин
    log.Printf("Enabling plugin: %s", plugin.Name)
    
    p, err := loadPlugin(plugin.Name, plugin.Config)
    if err != nil {
        return err
    }
    
    a.plugins[plugin.Name] = p
    
    return nil
}

func (a *Agent) disablePlugin(name string) {
    log.Printf("Disabling plugin: %s", name)
    
    if p, exists := a.plugins[name]; exists {
        p.Shutdown()
        delete(a.plugins, name)
    }
}
```


______________________________________________________________________

## 6. CLI для hot reload

**Утилита для управления конфигами:**

```go
// cmd/pi-config/main.go
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    
    "github.com/spf13/cobra"
)

var (
    configsDir string
    sessionID  int64
)

func main() {
    var rootCmd = &cobra.Command{
        Use:   "pi-config",
        Short: "Manage Pi agent configurations",
    }
    
    // reload command
    var reloadCmd = &cobra.Command{
        Use:   "reload",
        Short: "Reload configuration for a session",
        RunE: func(cmd *cobra.Command, args []string) error {
            configPath := filepath.Join(configsDir, fmt.Sprintf("session-%d.yaml", sessionID))
            
            // Проверить существование
            if _, err := os.Stat(configPath); os.IsNotExist(err) {
                return fmt.Errorf("config not found: %s", configPath)
            }
            
            // Отправить SIGHUP процессу
            return sendSIGHUP(sessionID)
        },
    }
    
    reloadCmd.Flags().Int64Var(&sessionID, "session", 0, "Session ID")
    reloadCmd.Flags().StringVar(&configsDir, "configs", "/tmp/daemon/pi-configs", "Configs directory")
    reloadCmd.MarkFlagRequired("session")
    
    // update command
    var updateCmd = &cobra.Command{
        Use:   "update",
        Short: "Update configuration",
        RunE: func(cmd *cobra.Command, args []string) error {
            configPath := filepath.Join(configsDir, fmt.Sprintf("session-%d.yaml", sessionID))
            
            // Загрузить текущий конфиг
            data, err := os.ReadFile(configPath)
            if err != nil {
                return err
            }
            
            var config PiConfig
            if err := yaml.Unmarshal(data, &config); err != nil {
                return err
            }
            
            // Вывести текущий конфиг
            pretty, _ := json.MarshalIndent(config, "", "  ")
            fmt.Println(string(pretty))
            
            // Здесь можно добавить интерактивное редактирование
            // или загрузку из файла
            
            return nil
        },
    }
    
    updateCmd.Flags().Int64Var(&sessionID, "session", 0, "Session ID")
    updateCmd.Flags().StringVar(&configsDir, "configs", "/tmp/daemon/pi-configs", "Configs directory")
    updateCmd.MarkFlagRequired("session")
    
    // validate command
    var validateCmd = &cobra.Command{
        Use:   "validate",
        Short: "Validate configuration",
        RunE: func(cmd *cobra.Command, args []string) error {
            configPath := filepath.Join(configsDir, fmt.Sprintf("session-%d.yaml", sessionID))
            
            data, err := os.ReadFile(configPath)
            if err != nil {
                return err
            }
            
            var config PiConfig
            if err := yaml.Unmarshal(data, &config); err != nil {
                return fmt.Errorf("invalid YAML: %w", err)
            }
            
            // Валидировать
            if err := validateConfig(&config); err != nil {
                return fmt.Errorf("validation failed: %w", err)
            }
            
            fmt.Println("Configuration is valid")
            
            return nil
        },
    }
    
    validateCmd.Flags().Int64Var(&sessionID, "session", 0, "Session ID")
    validateCmd.Flags().StringVar(&configsDir, "configs", "/tmp/daemon/pi-configs", "Configs directory")
    validateCmd.MarkFlagRequired("session")
    
    rootCmd.AddCommand(reloadCmd)
    rootCmd.AddCommand(updateCmd)
    rootCmd.AddCommand(validateCmd)
    
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}

func sendSIGHUP(sessionID int64) error {
    // Найти PID через tmux
    cmd := exec.Command(
        "tmux",
        "-L", "daemon",
        "list-panes",
        "-t", fmt.Sprintf("pi-%d", sessionID),
        "-F", "#{pane_pid}",
    )
    
    output, err := cmd.Output()
    if err != nil {
        return err
    }
    
    pid, _ := strconv.Atoi(strings.TrimSpace(string(output)))
    
    // Отправить сигнал
    process, err := os.FindProcess(pid)
    if err != nil {
        return err
    }
    
    return process.Signal(syscall.SIGHUP)
}
```

**Использование:**

```bash
# Перезагрузить конфиг
pi-config reload --session 123

# Проверить валидность
pi-config validate --session 123

# Посмотреть текущий конфиг
pi-config update --session 123
```


______________________________________________________________________

## Рекомендации

### 1. **Всегда валидировать перед reload**

- Проверять модель, плагины, MCP серверы
- Откатывать при ошибке валидации


### 2. **Graceful reload с таймаутом**

- Давать время на применение конфига
- Проверять health после reload


### 3. **Логировать все изменения**

- Что изменилось
- Успешно ли применилось
- Был ли откат


### 4. **Бэкапы конфигов**

- Хранить последние N версий
- Возможность отката вручную


### 5. **Rate limiting**

- Ограничить частоту reload (не чаще 1 раза в 10 сек)
- Избегать race conditions

