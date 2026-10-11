// Package runtime — адаптеры запуска агент-сессий (process/tmux/pi).
//
// Container-адаптер (Docker/k8s) — вне этой сборки (ТЗ 08, финальный проект);
// его создание возвращает явную ошибку.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// SessionState — состояние сессии (совпадает с БД/контрактом).
type SessionState string

const (
	StateStarting SessionState = "starting"
	StateRunning  SessionState = "running"
	StateIdle     SessionState = "idle"
	StateStopping SessionState = "stopping"
	StateStopped  SessionState = "stopped"
	StateFailed   SessionState = "failed"
)

// Spec — параметры запуска сессии.
type Spec struct {
	SessionID  int64
	RoleName   string
	WorkingDir string
	Command    string   // полная команда (process) или команда внутри tmux/pi
	Args       []string // аргументы (process)
	Env        []string // K=V
	OutputFile string   // куда писать stdout+stderr (transcript)
	ConfigFile string   // путь к сгенерированному конфиг-файлу (pi, для справки)
	ConfigsDir string
	Pi *AgentSpecFields // QA-fix: поля agent spec для pi — адаптер строит из них CLI-флаги
	// DirectTTY — запускать команду без пайпа в лог-файл (stdout = TTY).
	// QA-fix: TUI-приложения (pi) требуют TTY на stdout; `| tee` ломает их.
	// Для DirectTTY транскрипт берётся через capture-pane, не через лог-файл.
	DirectTTY bool
}

// Status — состояние хендла.
type Status struct {
	Alive    bool
	ExitCode *int
	State    SessionState
}

// P2.11 — send to session: результат ввода в терминал сессии.
type SendResult struct {
	Sent      bool
	Submitted bool
}

// P2.8 — capture (стл-кадр экрана сессии): sentinel-ошибки (маппинг на 409 в service).
var (
	// ErrNoScreen — у адаптера нет «экрана» (process); service использует log-tail.
	ErrNoScreen = errors.New("runtime has no screen")
	// ErrTmuxUnavailable — tmux не установлен.
	ErrTmuxUnavailable = errors.New("tmux is not installed")
	// ErrTmuxSessionDead — tmux-сессия мертва/отсутствует.
	ErrTmuxSessionDead = errors.New("tmux session not alive")
)

// Adapter — интерфейс runtime-адаптера.
type Adapter interface {
	Name() string
	Start(ctx context.Context, spec Spec) (runtimeRef string, err error)
	Stop(ctx context.Context, runtimeRef string) error
	Status(ctx context.Context, runtimeRef string) (*Status, error)
	// Capture — стл-кадр экрана (последние lines строк). process → ErrNoScreen.
	Capture(ctx context.Context, runtimeRef string, lines int) ([]string, error)
	// Send — ввести текст в терминал сессии (literal send-keys) + Enter при submit.
	// process → ErrNoScreen; tmux нет → ErrTmuxUnavailable; сессия мертва → ErrTmuxSessionDead.
	Send(ctx context.Context, runtimeRef, text string, submit bool) (SendResult, error)
}

// Registry — фабрика адаптеров.
type Registry struct {
	process   *ProcessAdapter
	tmux      *TMuxAdapter
	pi        *PiAdapter
	mu        sync.RWMutex
	overrides map[string]Adapter // тестовый хук: подмена адаптера по runtime-type
}

func NewRegistry() *Registry {
	p := &ProcessAdapter{}
	return &Registry{
		process: p,
		tmux:    &TMuxAdapter{Process: p},
		pi:      &PiAdapter{TMux: &TMuxAdapter{Process: p}},
	}
}

// SetOverride — тестовый хук: подменить адаптер для конкретного runtime-type
// (чтобы service-тесты управляли Capture/Send детерминированно, без реального tmux).
func (r *Registry) SetOverride(runtimeType string, a Adapter) {
	if r == nil || a == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.overrides == nil {
		r.overrides = map[string]Adapter{}
	}
	r.overrides[runtimeType] = a
}

// Get возвращает адаптер по типу; container/k8s — не поддерживаются в этой сборке.
func (r *Registry) Get(runtimeType string) (Adapter, error) {
	r.mu.RLock()
	if ov, ok := r.overrides[runtimeType]; ok {
		r.mu.RUnlock()
		return ov, nil
	}
	r.mu.RUnlock()
	switch runtimeType {
	case "process", "":
		return r.process, nil
	case "tmux":
		return r.tmux, nil
	case "pi":
		return r.pi, nil
	case "container", "k8s":
		return nil, fmt.Errorf("runtime %q is not supported in this build (planned in the final project)", runtimeType)
	default:
		return nil, fmt.Errorf("unknown runtime type %q", runtimeType)
	}
}
