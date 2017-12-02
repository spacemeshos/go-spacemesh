package log

import (
	"gopkg.in/op/go-logging.v1"
	"os"
)

var (

	// default app-level logger
	log = logging.MustGetLogger("app")

	// Example format string. Everything except the message has a custom color
	// which is dependent on the log level. Many fields have a custom output
	// formatting too, eg. the time returns the hour down to the milli second.
	logFormat = logging.MustStringFormatter(`%{color}%{time:15:04:05.000} %{shortpkg} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`, )
)

func init() {
	// we wrap all log calls so we need to add 1 to call depth
	log.ExtraCalldepth = 1
	backend := logging.NewLogBackend(os.Stderr, "", 0)
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)
	logging.SetBackend(backendFormatter)
}

// public wrappers

func Info(format string, args ...interface{}) {
	log.Info(format, args...)
}

func Debug(format string, args ...interface{}) {
	log.Debug(format, args...)
}

func Error(format string, args ...interface{}) {
	log.Error(format, args...)
}
