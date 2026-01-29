package logger

import (
	"fmt"
	"os"
	"time"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorWhite  = "\033[37m"
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorPurple = "\033[35m"
)

const (
	prefixSSLLY = "SSLLY-NGINX"
	prefixNginx = "NGINX-PROCS"
)

// Info logs an informational message
func Info(format string, args ...any) {
	log(prefixSSLLY, "INFO", colorCyan, format, args...)
}

// Warn logs a warning message
func Warn(format string, args ...any) {
	log(prefixSSLLY, "WARN", colorYellow, format, args...)
}

// Error logs an error message
func Error(format string, args ...any) {
	log(prefixSSLLY, "ERROR", colorRed, format, args...)
}

// Fatal logs an error message and exits
func Fatal(format string, args ...any) {
	log(prefixSSLLY, "ERROR", colorRed, format, args...)
	os.Exit(1)
}

// NginxInfo logs nginx process output as info
func NginxInfo(format string, args ...any) {
	log(prefixNginx, "INFO", colorCyan, format, args...)
}

// log formats and prints a log message with colours
func log(prefix, level, levelColor, format string, args ...any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)

	prefixColor := colorPurple
	if prefix == prefixNginx {
		prefixColor = colorGreen
	}

	fmt.Printf("%s[%s]%s %s[%s]%s %s[%s]%s %s%s%s\n",
		prefixColor, prefix, colorReset,
		colorWhite, timestamp, colorReset,
		levelColor, level, colorReset,
		colorWhite, message, colorReset,
	)
}

// NginxWriter wraps nginx stdout/stderr to log through our logger
type NginxWriter struct{}

// Write implements io.Writer interface
func (w *NginxWriter) Write(p []byte) (n int, err error) {
	if len(p) > 0 {
		// Remove trailing newline if present
		msg := string(p)
		if len(msg) > 0 && msg[len(msg)-1] == '\n' {
			msg = msg[:len(msg)-1]
		}
		if msg != "" {
			NginxInfo("%s", msg)
		}
	}
	return len(p), nil
}

// NewNginxWriter creates a new NginxWriter
func NewNginxWriter() *NginxWriter {
	return &NginxWriter{}
}
