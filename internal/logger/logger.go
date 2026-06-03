package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

var (
	infoLog  *log.Logger
	warnLog  *log.Logger
	errLog   *log.Logger
	logFile  *os.File
)

// Init initializes the logger to write to both console and logs/app.log
func Init() error {
	logDir := "logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, "app.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	logFile = file

	// MultiWriter to output to both console and file
	multiWriter := io.MultiWriter(os.Stdout, logFile)

	infoLog = log.New(multiWriter, "[INFO] ", log.Ldate|log.Ltime)
	warnLog = log.New(multiWriter, "[WARN] ", log.Ldate|log.Ltime)
	errLog = log.New(multiWriter, "[ERROR] ", log.Ldate|log.Ltime|log.Lshortfile)

	return nil
}

// Close closes the log file.
func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

// Info logs informational messages.
func Info(format string, v ...interface{}) {
	if infoLog == nil {
		fmt.Printf("[INFO] "+format+"\n", v...)
		return
	}
	infoLog.Printf(format, v...)
}

// Warn logs warning messages.
func Warn(format string, v ...interface{}) {
	if warnLog == nil {
		fmt.Printf("[WARN] "+format+"\n", v...)
		return
	}
	warnLog.Printf(format, v...)
}

// Error logs error messages.
func Error(format string, v ...interface{}) {
	if errLog == nil {
		fmt.Printf("[ERROR] "+format+"\n", v...)
		return
	}
	errLog.Printf(format, v...)
}
