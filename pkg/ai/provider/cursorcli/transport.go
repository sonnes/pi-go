package cursorcli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type sendArgs struct {
	prompt string
}

func spawn(
	ctx context.Context,
	cfg config,
	args sendArgs,
) (io.ReadCloser, func() error, error) {
	cliArgs := buildArgs(cfg, args)
	cmd := exec.Command(cfg.cliPath, cliArgs...)

	if cfg.workDir != "" {
		cmd.Dir = cfg.workDir
	}
	cmd.Env = os.Environ()
	if len(cfg.env) > 0 {
		cmd.Env = append(cmd.Env, cfg.env...)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("cursor: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("cursor: start: %w", err)
	}

	var (
		waitOnce sync.Once
		waitErr  error
		waitCh   = make(chan struct{})
	)
	doWait := func() {
		waitOnce.Do(func() {
			waitErr = cmd.Wait()
			close(waitCh)
		})
	}

	cleanup := func() error {
		doWait()
		<-waitCh
		if waitErr != nil && stderr.Len() > 0 {
			return fmt.Errorf("cursor: %w: %s", waitErr, stderr.String())
		}
		return waitErr
	}

	go func() {
		select {
		case <-ctx.Done():
			gracefulShutdown(cmd, waitCh)
			doWait()
		case <-waitCh:
		}
	}()

	return stdout, cleanup, nil
}

func buildArgs(cfg config, args sendArgs) []string {
	var a []string

	if cfg.apiKey != "" {
		a = append(a, "--api-key", cfg.apiKey)
	}
	for _, header := range cfg.headers {
		a = append(a, "-H", header)
	}

	a = append(a,
		"-p",
		"--output-format", "stream-json",
	)
	if cfg.model != "" {
		a = append(a, "--model", cfg.model)
	}
	if cfg.mode != "" {
		a = append(a, "--mode", cfg.mode)
	}
	if cfg.sandbox != "" {
		a = append(a, "--sandbox", cfg.sandbox)
	}
	if cfg.workDir != "" {
		a = append(a, "--workspace", cfg.workDir)
	}
	if cfg.force {
		a = append(a, "--force")
	}
	if cfg.approveMCPs {
		a = append(a, "--approve-mcps")
	}
	if cfg.browser {
		a = append(a, "--browser")
	}
	if args.prompt != "" {
		a = append(a, args.prompt)
	}

	return a
}

func gracefulShutdown(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd.Process == nil {
		return
	}

	_ = cmd.Process.Signal(syscall.SIGINT)

	select {
	case <-done:
		return
	case <-time.After(3 * time.Second):
	}

	_ = cmd.Process.Kill()
}
