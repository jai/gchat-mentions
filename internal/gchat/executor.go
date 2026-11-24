// Package gchat polls unread Google Chat mentions through the gog CLI.
package gchat

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Executor runs external CLI commands (mockable in tests).
type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// CLIExecutor runs commands via os/exec.
type CLIExecutor struct {
	ExtraEnv map[string]string
}

// NewCLIExecutor returns a CLIExecutor backed by os/exec.
func NewCLIExecutor() *CLIExecutor {
	return &CLIExecutor{}
}

// NewCLIExecutorWithEnv returns a CLIExecutor that injects extra environment
// variables (such as keyring settings) into each child command.
func NewCLIExecutorWithEnv(extra map[string]string) *CLIExecutor {
	cp := make(map[string]string, len(extra))
	for k, v := range extra {
		cp[k] = v
	}
	return &CLIExecutor{ExtraEnv: cp}
}

// Run executes name with args and returns stdout.
func (e *CLIExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(e.ExtraEnv) > 0 {
		env := os.Environ()
		for k, v := range e.ExtraEnv {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return stderr.Bytes(), fmt.Errorf("%w: %s", err, msg)
		}
		return stderr.Bytes(), err
	}
	return stdout.Bytes(), nil
}
