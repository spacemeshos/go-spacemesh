// Package log provides the both file and console (general) logging capabilities
// to spacemesh modules such as app and node.
package log

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/op/go-logging.v1"
)

const colorString = "\x1b[38;2;{r};{g};{b}m"
const colorBgString = "\x1b[48;2;{r};{g};{b}m"
const resetColorString = "\x1b[0m"

var usedColors = make(map[string]bool)

func createRandomColor(txt string) string {

	randomized := func() string {
		rand.Seed(time.Now().UnixNano())
		r := strconv.Itoa(rand.Intn(255))
		g := strconv.Itoa(rand.Intn(255))
		b := strconv.Itoa(rand.Intn(255))
		color := strings.Replace(colorString, "{r}", r, 1)
		color = strings.Replace(color, "{g}", g, 1)
		color = strings.Replace(color, "{b}", b, 1)
		return color
	}
	randColor := randomized()
	for usedColors[randColor] {
		randColor = randomized()
	}

	return randColor + txt + resetColorString
}

// SpacemeshLogger is a custom logger.
type SpacemeshLogger struct {
	Logger *logging.Logger
}

// smlogger is the local app singleton logger.
var smLogger *SpacemeshLogger

func init() {
	// create a basic temp os.Stdout logger
	// This logger is used until the app calls InitSpacemeshLoggingSystem().

	log := logging.MustGetLogger("app")
	log.ExtraCalldepth = 1
	logFormat := logging.MustStringFormatter(` %{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} ▶%{color:reset} %{message}`)
	backend := logging.NewLogBackend(os.Stdout, "<APP>", 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)
	logging.SetBackend(backendFormatter)
	smLogger = &SpacemeshLogger{Logger: log}

	log.Info("Spacemesh uses 256 terminal colors. please make sure your terminal supports it")
	log.Info(createRandomColor("T") + createRandomColor("E") + createRandomColor("S") + createRandomColor("T"))
	usedColors = make(map[string]bool)
}

// CreateLogger creates a logger for a module. e.g. local node logger.
func CreateLogger(module string, dataFolderPath string, logFileName string) *logging.Logger {

	log := logging.MustGetLogger(module)
	log.ExtraCalldepth = 1
	logFormat := logging.MustStringFormatter(` %{color:reset}%{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} %{shortpkg}.%{shortfunc} ▶%{color:reset} %{message}`)
	// module name is set is message prefix
	backend := logging.NewLogBackend(os.Stdout, createRandomColor(module), 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)

	fileName := filepath.Join(dataFolderPath, logFileName)

	fileLogger := &lumberjack.Logger{
		Filename:   fileName,
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   false,
	}

	fileLoggerBackend := logging.NewLogBackend(fileLogger, "", 0)
	logFileFormat := logging.MustStringFormatter(`%{time:15:04:05.000} %{level:.4s} %{id:03x} %{shortpkg}.%{shortfunc} ▶ %{message}`)
	fileBackendFormatter := logging.NewBackendFormatter(fileLoggerBackend, logFileFormat)

	backendConsoleLevel := logging.AddModuleLevel(backendFormatter)
	backendFileLevel := logging.AddModuleLevel(fileBackendFormatter)
	log.SetBackend(logging.MultiLogger(backendConsoleLevel, backendFileLevel))

	return log
}

// InitSpacemeshLoggingSystem initializes app logging system.
func InitSpacemeshLoggingSystem(dataFolderPath string, logFileName string) {

	log := logging.MustGetLogger("app")

	// we wrap all log calls so we need to add 1 to call depth
	log.ExtraCalldepth = 1

	logFormat := logging.MustStringFormatter(` %{color}%{level:.4s} %{id:03x} %{time:15:04:05.000} %{shortpkg}.%{shortfunc}%{color:reset} ▶ %{message}`)
	backend := logging.NewLogBackend(os.Stdout, "<APP>", 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)

	fileName := filepath.Join(dataFolderPath, logFileName)

	fileLogger := &lumberjack.Logger{
		Filename:   fileName,
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   false,
	}

	fileLoggerBackend := logging.NewLogBackend(fileLogger, "", 0)
	logFileFormat := logging.MustStringFormatter(`%{time:15:04:05.000} %{level:.4s}-%{id:03x} %{shortpkg}.%{shortfunc} ▶ %{message}`)
	fileBackendFormatter := logging.NewBackendFormatter(fileLoggerBackend, logFileFormat)

	backendConsoleLevel := logging.AddModuleLevel(backendFormatter)
	backendFileLevel := logging.AddModuleLevel(fileBackendFormatter)
	log.SetBackend(logging.MultiLogger(backendConsoleLevel, backendFileLevel))

	smLogger = &SpacemeshLogger{Logger: log}
}

// public wrappers abstracting away logging lib impl

// Info prints formatted info level log message.
func Info(format string, args ...interface{}) {
	smLogger.Logger.Info(format, args...)
}

// Debug prints formatted debug level log message.
func Debug(format string, args ...interface{}) {
	smLogger.Logger.Debug(format, args...)
}

// Error prints formatted error level log message.
func Error(format string, args ...interface{}) {
	smLogger.Logger.Error(format, args...)
}

// Warning prints formatted warning level log message.
func Warning(format string, args ...interface{}) {
	smLogger.Logger.Warning(format, args...)
}

// PrettyID formats ID.
func PrettyID(id string) string {
	m := 6
	if len(id) < m {
		m = len(id)
	}
	return fmt.Sprintf("<ID %s>", id[:m])
}
