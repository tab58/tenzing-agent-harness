package nexus

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const commandHealthyReset = 60 * time.Second

// runCommand spawns argv and feeds each stdout/stderr line to ingest. When
// the process exits it restarts with exponential backoff (restartBase
// doubling up to restartCap, reset after a healthy run of
// commandHealthyReset). Cancelling ctx kills the child and returns.
func runCommand(ctx context.Context, argv []string, restartBase, restartCap time.Duration, ingest func(string), setStatus func(string)) {
	defer setStatus("stopped")

	backoff := restartBase
	for {
		setStatus("running")
		start := time.Now()
		runCommandOnce(ctx, argv, ingest)

		if ctx.Err() != nil {
			return
		}
		if time.Since(start) >= commandHealthyReset {
			backoff = restartBase
		}

		setStatus("restarting")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > restartCap {
			backoff = restartCap
		}
	}
}

// runCommandOnce runs a single incarnation of the command to completion or
// ctx cancellation.
func runCommandOnce(ctx context.Context, argv []string, ingest func(string)) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// Own process group so cancellation can kill grandchildren too (e.g.
	// children of `sh -c` wrappers). ponytail: unix-only; Windows needs a
	// job object if that ever matters.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}

	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			if line := sc.Text(); line != "" {
				ingest(line)
			}
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)

	// Wait for scanners with context cancellation
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Scanners finished naturally
	case <-ctx.Done():
		// Context cancelled: kill the whole process group (Setpgid above)
		// so grandchildren die too, then unblock the scanners.
		if cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		stdout.Close()
		stderr.Close()
		<-done // Wait for scanners to exit from EOF
	}

	cmd.Wait()
}
