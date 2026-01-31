package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	ssllyMinLevel      = LevelInfo
	nginxMinLevel      = LevelInfo
	nginxStderrAsLevel = LevelError // Default: treat nginx stderr as error level
	logDir             string
	logFile            *os.File
	logMu              sync.Mutex
	currentDate        string
)

// SetSSLLYLevel sets the minimum log level for SSLLY-NGINX logs
func SetSSLLYLevel(level string) {
	ssllyMinLevel = parseLevel(level)
}

// SetNginxLevel sets the minimum log level for NGINX-PROCS logs
func SetNginxLevel(level string) {
	nginxMinLevel = parseLevel(level)
}

// SetNginxStderrLevel sets the log level for nginx stderr output (warn or error)
func SetNginxStderrLevel(level string) {
	nginxStderrAsLevel = parseLevel(level)
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

// InitFileLogging initializes file logging with a session directory and daily log files
func InitFileLogging() error {
	logMu.Lock()
	defer logMu.Unlock()

	// Create session directory named by startup time
	sessionTime := time.Now().Format("20060102_150405")
	logDir = filepath.Join("/app/logs", sessionTime)
	if err := os.MkdirAll(logDir, 0777); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Open today's log file
	return openLogFile()
}

// openLogFile opens or switches to today's log file
func openLogFile() error {
	today := time.Now().Format("2006-01-02")
	if currentDate == today && logFile != nil {
		return nil // Already using today's file
	}

	// Close previous file if exists
	if logFile != nil {
		_ = logFile.Close()
	}

	// Open new file for today
	logFilePath := filepath.Join(logDir, today+".log")
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logFile = file
	currentDate = today
	return nil
}

// Debug logs a debug message
func Debug(format string, args ...any) {
	if ssllyMinLevel <= LevelDebug {
		log(prefixSSLLY, "DEBUG", colorWhite, format, args...)
	}
}

// Info logs an informational message
func Info(format string, args ...any) {
	if ssllyMinLevel <= LevelInfo {
		log(prefixSSLLY, "INFO", colorCyan, format, args...)
	}
}

// Warn logs a warning message
func Warn(format string, args ...any) {
	if ssllyMinLevel <= LevelWarn {
		log(prefixSSLLY, "WARN", colorYellow, format, args...)
	}
}

// Error logs an error message
func Error(format string, args ...any) {
	if ssllyMinLevel <= LevelError {
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

// NginxError logs nginx process stderr as warning
func NginxWarn(format string, args ...any) {
	if nginxMinLevel <= LevelWarn {
		log(prefixNginx, "WARN", colorYellow, format, args...)
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

	// Format with colors for console
	coloredLine := fmt.Sprintf("%s[%s]%s %s[%s]%s %s[%s]%s %s%s%s\n",
		prefixColor, prefix, colorReset,
		colorWhite, timestamp, colorReset,
		levelColor, level, colorReset,
		colorWhite, message, colorReset,
	)

	// Format without colors for file
	plainLine := fmt.Sprintf("[%s] [%s] [%s] %s\n", prefix, timestamp, level, message)

	// Output to console
	fmt.Print(coloredLine)

	// Output to file if initialized
	logMu.Lock()
	defer logMu.Unlock()

	// Check if we need to switch log file (new day)
	if logFile != nil {
		_ = openLogFile()
	}

	if logFile != nil {
		_, _ = io.WriteString(logFile, plainLine)
	}
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
				// Route stderr to Warn or Error based on nginxStderrAsLevel configuration
				if nginxStderrAsLevel == LevelWarn {
					NginxWarn("%s", msg)
				} else {
					NginxError("%s", msg)
				}
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
