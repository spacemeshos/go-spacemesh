package logger

import (
	"gopkg.in/op/go-logging.v1"
	"os"
)

var (

	// default app-level logger
	Log = logging.MustGetLogger("app")

	// Example format string. Everything except the message has a custom color
	// which is dependent on the log level. Many fields have a custom output
	// formatting too, eg. the time returns the hour down to the milli second.
	logFormat = logging.MustStringFormatter(`%{color}%{time:15:04:05.000} %{shortfunc} ▶ %{level:.4s} %{id:03x}%{color:reset} %{message}`, )
)

func init() {

	// For demo purposes, create two backend for os.Stderr.
	backend := logging.NewLogBackend(os.Stderr, "", 0)

	// For messages written to backend2 we want to add some additional
	// information to the output, including the used log level and the name of
	// the function.
	backendFormatter := logging.NewBackendFormatter(backend, logFormat)

	// Set the backends to be used.
	logging.SetBackend(backendFormatter)

	Log.Debugf("debug %s", "todo: fix me")
	Log.Info("info")
	Log.Notice("notice")
	Log.Warning("warning")
	Log.Error("err")
	Log.Critical("crit")
}
