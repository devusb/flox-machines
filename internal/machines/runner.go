package machines

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if msg == "" {
				msg = err.Error()
			}
			return out, &ExitError{Code: exit.ExitCode(), Msg: msg}
		}
		if msg != "" {
			return out, errors.New(msg)
		}
		return out, err
	}
	return out, nil
}

type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Msg
}
