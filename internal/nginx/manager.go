package nginx

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/hnrobert/sslly-nginx/internal/logger"
)

type Manager struct {
	cmd *exec.Cmd
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Start() error {
	logger.Info("Starting nginx...")

	// Remove stale PID file if it exists (we use /tmp for non-root compatibility)
	_ = os.Remove("/tmp/nginx.pid")

	// Ensure nginx temp directories are writable in non-root containers.
	_ = os.MkdirAll("/tmp/nginx/client_body", 0777)
	_ = os.MkdirAll("/tmp/nginx/proxy", 0777)
	_ = os.MkdirAll("/tmp/nginx/fastcgi", 0777)
	_ = os.MkdirAll("/tmp/nginx/uwsgi", 0777)
	_ = os.MkdirAll("/tmp/nginx/scgi", 0777)

	cmd := exec.Command("nginx", "-g", "daemon off;")
	// Important: by default, os/exec discards child stdout/stderr.
	// Pipe nginx logs through our logger with [NGINX-PROCS] prefix.
	cmd.Stdout = logger.NewNginxStdoutWriter()
	cmd.Stderr = logger.NewNginxStderrWriter()

	// Start nginx in background
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start nginx: %w", err)
	}

	m.cmd = cmd

	// Wait a moment for nginx to start.
	time.Sleep(2 * time.Second)

	return nil
}

func (m *Manager) Stop() {
	if m.cmd != nil && m.cmd.Process != nil {
		logger.Info("Stopping nginx...")
		m.cmd.Process.Kill()
	}
}

func (m *Manager) Reload() error {
	logger.Info("Reloading nginx...")

	// Test configuration first
	cmd := exec.Command("nginx", "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx configuration test failed: %s", string(output))
	}

	// Use kill -HUP to reload nginx gracefully instead of nginx -s reload
	// This is more reliable when nginx is running in non-daemon mode
	if m.cmd != nil && m.cmd.Process != nil {
		if err := m.cmd.Process.Signal(os.Signal(syscall.SIGHUP)); err != nil {
			return fmt.Errorf("failed to send SIGHUP to nginx: %w", err)
		}
	} else {
		return fmt.Errorf("nginx process not found")
	}

	return nil
}

func (m *Manager) CheckHealth() error {
	// Test nginx configuration
	cmd := exec.Command("nginx", "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx health check failed: %s", string(output))
	}

	return nil
}
