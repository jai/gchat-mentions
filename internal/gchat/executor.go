// Package gchat polls unread Google Chat mentions through the gog CLI.
package gchat

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Executor runs external CLI commands (mockable in tests).
type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// CLIExecutor runs commands via os/exec.
type CLIExecutor struct{}

// NewCLIExecutor returns a CLIExecutor backed by os/exec.
func NewCLIExecutor() *CLIExecutor {
	return &CLIExecutor{}
}

// Run executes name with args and returns stdout.
func (e *CLIExecutor) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
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
