package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/hnrobert/sslly-nginx/internal/app"
	"github.com/hnrobert/sslly-nginx/internal/logger"
)

func main() {
	// Initialize file logging
	if err := logger.InitFileLogging(); err != nil {
		logger.Warn("Failed to initialize file logging: %v", err)
	}

	logger.Info("Starting sslly-nginx...")

	application, err := app.New()
	if err != nil {
		logger.Fatal("Failed to create application: %v", err)
	}

	if err := application.Start(); err != nil {
		logger.Fatal("Failed to start application: %v", err)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down sslly-nginx...")
	application.Stop()
}
