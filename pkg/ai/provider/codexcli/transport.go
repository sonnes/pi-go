package codexcli

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
	prompt           string
	ephemeral        bool
	outputSchemaPath string
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
		return nil, nil, fmt.Errorf("codex: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("codex: start: %w", err)
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
			return fmt.Errorf("codex: %w: %s", waitErr, stderr.String())
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

	if cfg.approvalPolicy != "" {
		a = append(a, "--ask-for-approval", cfg.approvalPolicy)
	}

	a = append(a,
		"exec",
		"--json",
		"--color", "never",
	)

	if args.ephemeral {
		a = append(a, "--ephemeral")
	}
	if cfg.model != "" {
		a = append(a, "--model", cfg.model)
	}
	if cfg.workDir != "" {
		a = append(a, "--cd", cfg.workDir)
	}
	for _, dir := range cfg.addDirs {
		a = append(a, "--add-dir", dir)
	}
	if cfg.sandbox != "" {
		a = append(a, "--sandbox", cfg.sandbox)
	}
	if cfg.skipGitRepoCheck {
		a = append(a, "--skip-git-repo-check")
	}
	if cfg.ignoreUserConfig {
		a = append(a, "--ignore-user-config")
	}
	if cfg.ignoreRules {
		a = append(a, "--ignore-rules")
	}
	if args.outputSchemaPath != "" {
		a = append(a, "--output-schema", args.outputSchemaPath)
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
