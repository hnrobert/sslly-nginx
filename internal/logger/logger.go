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
)

// Info logs an informational message
func Info(format string, args ...any) {
	log("INFO", colorCyan, format, args...)
}

// Warn logs a warning message
func Warn(format string, args ...any) {
	log("WARN", colorYellow, format, args...)
}

// Error logs an error message
func Error(format string, args ...any) {
	log("ERROR", colorRed, format, args...)
}

// Fatal logs an error message and exits
func Fatal(format string, args ...any) {
	log("ERROR", colorRed, format, args...)
	os.Exit(1)
}

// log formats and prints a log message with colours
func log(level, levelColor, format string, args ...any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)

	fmt.Printf("%s[SSLLY-NGINX]%s %s[%s]%s %s[%s]%s %s%s%s\n",
		colorGreen, colorReset,
		colorWhite, timestamp, colorReset,
		levelColor, level, colorReset,
		colorWhite, message, colorReset,
	)
}
