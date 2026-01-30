package logger

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	_ = r.Close()
	return buf.String()
}

func TestInfoWarnError(t *testing.T) {
	out := captureStdout(t, func() {
		Info("hello %s", "world")
		Warn("warn %d", 1)
		Error("err")
	})
	if !strings.Contains(out, "[SSLLY-NGINX]") {
		t.Fatalf("expected prefix, got: %q", out)
	}
	if !strings.Contains(out, "INFO") || !strings.Contains(out, "WARN") || !strings.Contains(out, "ERROR") {
		t.Fatalf("expected log levels in output, got: %q", out)
	}
}

func TestFatalExits(t *testing.T) {
	if os.Getenv("SSLLY_TEST_FATAL") == "1" {
		Fatal("boom")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalExits")
	cmd.Env = append(os.Environ(), "SSLLY_TEST_FATAL=1")
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-nil error (process should exit)")
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() != 1 {
			t.Fatalf("expected exit code 1, got %d", exitErr.ExitCode())
		}
		return
	}
	t.Fatalf("expected ExitError, got %T: %v", err, err)
}
