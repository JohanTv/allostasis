package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Runner encapsula la ejecución segura de comandos de Git con aislamiento de procesos.
type Runner struct {
	defaultTimeout time.Duration
}

// NewRunner crea un ejecutor de Git con un timeout predeterminado.
func NewRunner(timeout time.Duration) *Runner {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Runner{defaultTimeout: timeout}
}

// Run ejecuta un comando Git en un directorio de trabajo y retorna stdout, stderr y error.
func (r *Runner) Run(ctx context.Context, dir string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.defaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}

	// Aislamiento de grupo de procesos para matar hijos de git si se aborta
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			// Matar el grupo de procesos si el contexto expiró o fue cancelado
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			if ctx.Err() == context.DeadlineExceeded {
				return stdout.String(), stderr.String(), fmt.Errorf("git command timed out after %v", r.defaultTimeout)
			}
			return stdout.String(), stderr.String(), fmt.Errorf("git command canceled: %w", ctx.Err())
		}
		return stdout.String(), stderr.String(), fmt.Errorf("git %s failed: %w (stderr: %s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), nil
}

// GitVersion obtiene la versión actual de Git en el host.
func (r *Runner) GitVersion(ctx context.Context) (string, error) {
	out, _, err := r.Run(ctx, "", "version")
	if err != nil {
		return "", err
	}
	return out, nil
}
