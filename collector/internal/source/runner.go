package source

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Runner corre comandos externos sin shell.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner usa exec.CommandContext con LC_ALL=C y un timeout por comando
// (Timeout, salvo que ctx ya traiga su propio plazo).
// Si el binario no existe devuelve un error que cumple errors.Is(err, exec.ErrNotFound).
// Si el comando sale con código distinto de 0 devuelve la salida y un *exec.ExitError.
type ExecRunner struct{ Timeout time.Duration }

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, err
	}
	if _, ok := ctx.Deadline(); !ok {
		t := r.Timeout
		if t <= 0 {
			t = 2 * time.Second
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return out, context.DeadlineExceeded
	}
	return out, err
}

// Streamer lanza un comando de larga vida y entrega su salida estándar.
// Cerrar el lector termina el proceso.
type Streamer interface {
	Stream(ctx context.Context, name string, args ...string) (io.ReadCloser, error)
}

// Stream arranca el comando sin timeout; vive hasta que se cierra el lector,
// se cancela ctx o muere este proceso (Pdeathsig).
func (ExecRunner) Stream(ctx context.Context, name string, args ...string) (io.ReadCloser, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	return &stream{ReadCloser: out, cmd: cmd, cancel: cancel}, nil
}

type stream struct {
	io.ReadCloser
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func (s *stream) Close() error {
	s.cancel()
	return s.cmd.Wait()
}
