package feeds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// CustomProducer executes a shell command and parses stdout as JSON.
type CustomProducer struct {
	name    string
	command string
	timeout time.Duration
}

// NewCustomProducer creates a custom feed producer that runs the given shell
// command and expects JSON output on stdout.
func NewCustomProducer(name, command string, timeout time.Duration) *CustomProducer {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &CustomProducer{
		name:    name,
		command: command,
		timeout: timeout,
	}
}

// Name returns the custom feed name.
func (p *CustomProducer) Name() string {
	return p.name
}

// Produce executes the configured shell command with a timeout context.
// The command's stdout is parsed as JSON and returned.
// Returns an error if the command fails, times out, or produces invalid JSON.
func (p *CustomProducer) Produce(ctx context.Context) (interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", p.command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Kill the entire process group so child processes are cleaned up.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("custom feed %q: command timed out after %v", p.name, p.timeout)
		}
		return nil, fmt.Errorf("custom feed %q: command failed: %w (stderr: %s)",
			p.name, err, stderr.String())
	}

	var result interface{}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("custom feed %q: invalid JSON output: %w", p.name, err)
	}

	return map[string]interface{}{
		"type":      p.name,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      result,
	}, nil
}
