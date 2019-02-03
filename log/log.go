// Package log provides the both file and console (general) logging capabilities
// to spacemesh modules such as app and identity.
package log

import (
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)


// determine the level of messages we show.
var debugMode = false
// should we format out logs in json
const JSON = false // todo: configure


var DebugLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
	return lvl >= zapcore.DebugLevel
})

var InfoLevel = zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
	return lvl >= zapcore.InfoLevel
})

func logLevel() zap.LevelEnablerFunc {
	if debugMode {
		return DebugLevel
	} else {
		return InfoLevel
	}
}

func encoder() zapcore.Encoder {
	if JSON || debugMode {
		return zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig())
	} else {
		return zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
	}
}

// AppLog is the local app singleton logger.
var AppLog Log

func init() {
	// create a basic temp os.Stdout logger
	// This logger is used until the app calls InitSpacemeshLoggingSystem().
	AppLog = New("APP", "", "")
}

// DebugMode sets log debug level
func DebugMode(mode bool) {
	debugMode = mode
}

// New creates a logger for a module. e.g. p2p instance logger.
func New(module string, dataFolderPath string, logFileName string) Log {
	var cores []zapcore.Core

	consoleSyncer := zapcore.AddSync(os.Stdout)
	enc := encoder()

	cores = append(cores, zapcore.NewCore(enc, consoleSyncer, logLevel()))

	if dataFolderPath != "" && logFileName != "" {
		wr := getFileWriter(dataFolderPath, logFileName)
		fs := zapcore.AddSync(wr)
		cores = append(cores, zapcore.NewCore(enc, fs, DebugLevel))
	}

	core := zapcore.NewTee(cores...)

	log := zap.New(core)
	log = log.Named(module)
	return Log{log}
}

// getBackendLevelWithFileBackend returns backends level including log file backend
func getFileWriter(dataFolderPath, logFileName string) (io.Writer) {
	fileName := filepath.Join(dataFolderPath, logFileName)

	fileLogger := &lumberjack.Logger{
		Filename:   fileName,
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
		Compress:   false,
	}

	return fileLogger
}

// InitSpacemeshLoggingSystem initializes app logging system.
func InitSpacemeshLoggingSystem(dataFolderPath string, logFileName string) {
	AppLog = New("APP",  dataFolderPath, logFileName)
}

// public wrappers abstracting away logging lib impl

// Info prints formatted info level log message.
func Info(msg string, args ...interface{}) {
	AppLog.Info(msg, args...)
}

// Debug prints formatted debug level log message.
func Debug(msg string, args ...interface{}) {
	AppLog.Debug(msg, args...)
}

// Error prints formatted error level log message.
func Error(msg string, args ...interface{}) {
	AppLog.Error(msg, args...)
}

// Warning prints formatted warning level log message.
func Warning(msg string, args ...interface{}) {
	AppLog.Warning(msg, args...)
}

// Infow prints a structured info message
func Infow(msg string, fields ...Field) {
	AppLog.Infow(msg, fields...)
}

// Debugw prints a structured debug message
func Debugw(msg string, fields ...Field) {
	AppLog.Debugw(msg, fields...)
}

// Errorw prints a structured err message
func Errorw(msg string, fields ...Field) {
	AppLog.Errorw(msg, fields...)
}

// Warningw prints a structured warn message
func Warningw(msg string, fields ...Field) {
	AppLog.Warningw(msg, fields...)
}