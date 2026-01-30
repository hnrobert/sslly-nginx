package logger

import (
	"fmt"
	"os"
	"strings"
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

type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

var (
	ssllMinLevel  = LevelInfo
	nginxMinLevel = LevelInfo
)

// SetSSLLYLevel sets the minimum log level for SSLLY-NGINX logs
func SetSSLLYLevel(level string) {
	ssllMinLevel = parseLevel(level)
}

// SetNginxLevel sets the minimum log level for NGINX-PROCS logs
func SetNginxLevel(level string) {
	nginxMinLevel = parseLevel(level)
}

func parseLevel(level string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Debug logs a debug message
func Debug(format string, args ...any) {
	if ssllMinLevel <= LevelDebug {
		log(prefixSSLLY, "DEBUG", colorWhite, format, args...)
	}
}

// Info logs an informational message
func Info(format string, args ...any) {
	if ssllMinLevel <= LevelInfo {
		log(prefixSSLLY, "INFO", colorCyan, format, args...)
	}
}

// Warn logs a warning message
func Warn(format string, args ...any) {
	if ssllMinLevel <= LevelWarn {
		log(prefixSSLLY, "WARN", colorYellow, format, args...)
	}
}

// Error logs an error message
func Error(format string, args ...any) {
	if ssllMinLevel <= LevelError {
		log(prefixSSLLY, "ERROR", colorRed, format, args...)
	}
}

// Fatal logs an error message and exits
func Fatal(format string, args ...any) {
	log(prefixSSLLY, "ERROR", colorRed, format, args...)
	os.Exit(1)
}

// NginxInfo logs nginx process output as info
func NginxInfo(format string, args ...any) {
	if nginxMinLevel <= LevelInfo {
		log(prefixNginx, "INFO", colorCyan, format, args...)
	}
}

// NginxError logs nginx process stderr as error
func NginxError(format string, args ...any) {
	if nginxMinLevel <= LevelError {
		log(prefixNginx, "ERROR", colorRed, format, args...)
	}
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
type NginxWriter struct {
	isStderr bool
}

// Write implements io.Writer interface
func (w *NginxWriter) Write(p []byte) (n int, err error) {
	if len(p) > 0 {
		// Remove trailing newline if present
		msg := string(p)
		if len(msg) > 0 && msg[len(msg)-1] == '\n' {
			msg = msg[:len(msg)-1]
		}
		if msg != "" {
			if w.isStderr {
				NginxError("%s", msg)
			} else {
				NginxInfo("%s", msg)
			}
		}
	}
	return len(p), nil
}

// NewNginxStdoutWriter creates a new NginxWriter for stdout
func NewNginxStdoutWriter() *NginxWriter {
	return &NginxWriter{isStderr: false}
}

// NewNginxStderrWriter creates a new NginxWriter for stderr
func NewNginxStderrWriter() *NginxWriter {
	return &NginxWriter{isStderr: true}
}
