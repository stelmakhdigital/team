package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// ProcessAdapter — запуск локального процесса.
type ProcessAdapter struct {
	mu    sync.Mutex
	procs map[int]*procEntry // PID -> entry
}

type procEntry struct {
	cmd         *exec.Cmd
	exitCode    *int
	done        chan struct{} // закрыт после фиксации exit code (гонка reaper/Wait)
	stoppedByUs bool          // мы отправили SIGTERM (Stop) — stop ≠ crash
}

func (a *ProcessAdapter) Name() string { return "process" }

func (a *ProcessAdapter) Start(ctx context.Context, spec Spec) (string, error) {
	if spec.Command == "" {
		return "", fmt.Errorf("process runtime requires a command")
	}
	// процесс живёт независимо от контекста HTTP-запроса
	ctx = context.WithoutCancel(ctx)
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	cmd.Dir = spec.WorkingDir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	cmd.WaitDelay = 5 * time.Second

	var out *os.File
	if spec.OutputFile != "" {
		var err error
		out, err = os.OpenFile(spec.OutputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return "", fmt.Errorf("open output file: %w", err)
		}
		cmd.Stdout = out
		cmd.Stderr = out
	}
	if err := cmd.Start(); err != nil {
		if out != nil {
			out.Close()
		}
		return "", fmt.Errorf("start process: %w", err)
	}
	pid := cmd.Process.Pid
	entry := &procEntry{cmd: cmd, done: make(chan struct{})}

	a.mu.Lock()
	if a.procs == nil {
		a.procs = map[int]*procEntry{}
	}
	a.procs[pid] = entry
	a.mu.Unlock()

	// Фоновый wait: фиксируем exit code.
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = -1
			}
		}
		a.mu.Lock()
		entry.exitCode = &code
		a.mu.Unlock()
		close(entry.done)
		if out != nil {
			out.Close()
		}
	}()
	return strconv.Itoa(pid), nil
}

func (a *ProcessAdapter) Stop(ctx context.Context, runtimeRef string) error {
	pid, err := strconv.Atoi(runtimeRef)
	if err != nil {
		return fmt.Errorf("invalid runtime ref %q", runtimeRef)
	}
	a.mu.Lock()
	e, ok := a.procs[pid]
	// Фиксируем "stop от нас" только если процесс ещё не зафиксировал crash,
	// иначе user-stop после падения не затирает failed.
	if ok && e.exitCode == nil {
		e.stoppedByUs = true
	}
	a.mu.Unlock()
	if !ok {
		return nil // уже завершён
	}
	_ = ctx
	return e.cmd.Process.Signal(syscall.SIGTERM)
}

func (a *ProcessAdapter) Status(ctx context.Context, runtimeRef string) (*Status, error) {
	pid, err := strconv.Atoi(runtimeRef)
	if err != nil {
		return nil, fmt.Errorf("invalid runtime ref %q", runtimeRef)
	}
	a.mu.Lock()
	e, ok := a.procs[pid]
	var exit *int
	var stoppedByUs bool
	if ok {
		exit = e.exitCode
		stoppedByUs = e.stoppedByUs
	}
	a.mu.Unlock()
	// Гонка: процесс умер, но фоновый Wait ещё не зафиксировал exit code.
	// Ждём done (до 300мс), иначе отдаём Alive=true — следующий reap разберётся.
	if ok && exit == nil {
		select {
		case <-e.done:
		case <-time.After(300 * time.Millisecond):
			a.mu.Lock()
			exit = e.exitCode
			a.mu.Unlock()
			if exit == nil {
				return &Status{Alive: true, State: StateRunning}, nil
			}
		}
		a.mu.Lock()
		exit = e.exitCode
		stoppedByUs = e.stoppedByUs
		a.mu.Unlock()
	}
	if exit != nil {
		st := StateStopped
		if !stoppedByUs && *exit != 0 {
			st = StateFailed
		}
		return &Status{Alive: false, State: st, ExitCode: exit}, nil
	}
	// не знаем PID (daemon перезапустился) — спрашиваем ОС
	if p, err := os.FindProcess(pid); err == nil {
		if err := p.Signal(syscall.Signal(0)); err == nil {
			return &Status{Alive: true, State: StateRunning}, nil
		}
	}
	return &Status{Alive: false, State: StateStopped, ExitCode: intPtr(0)}, nil
}

func intPtr(v int) *int { return &v }
