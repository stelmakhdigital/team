// Package runtime — адаптеры запуска агент-сессий (process/tmux/pi).
//
// Container-адаптер (Docker/k8s) — вне этой сборки (ТЗ 08, финальный проект);
// его создание возвращает явную ошибку.
package runtime

import (
	"context"
	"fmt"
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
	ConfigFile string   // путь к сгенерированному конфиг-файлу (pi)
	ConfigsDir string
}

// Status — состояние хендла.
type Status struct {
	Alive    bool
	ExitCode *int
	State    SessionState
}

// Adapter — интерфейс runtime-адаптера.
type Adapter interface {
	Name() string
	Start(ctx context.Context, spec Spec) (runtimeRef string, err error)
	Stop(ctx context.Context, runtimeRef string) error
	Status(ctx context.Context, runtimeRef string) (*Status, error)
}

// Registry — фабрика адаптеров.
type Registry struct {
	process *ProcessAdapter
	tmux    *TMuxAdapter
	pi      *PiAdapter
}

func NewRegistry() *Registry {
	p := &ProcessAdapter{}
	return &Registry{
		process: p,
		tmux:    &TMuxAdapter{Process: p},
		pi:      &PiAdapter{TMux: &TMuxAdapter{Process: p}},
	}
}

// Get возвращает адаптер по типу; container/k8s — не поддерживаются в этой сборке.
func (r *Registry) Get(runtimeType string) (Adapter, error) {
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
