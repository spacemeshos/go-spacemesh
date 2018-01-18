package config

import (
	"time"
	"gopkg.in/urfave/cli.v1/altsrc"
)

// Provide Config struct with default values

// Config values with defaults
var ConfigValues = Config{
	AppIntParam:  20,
	AppBoolParam: true,
	DataFilePath: "~/.spacemesh",
}

func init() {
	// todo: update default config params based on runtime env here
}

type Config struct {
	ConfigFilePath string
	AppIntParam    int
	AppBoolParam   bool
	DataFilePath   string
}


func CastConfigUints(context altsrc.InputSourceContext, flagstovalues []map[string]*uint) {
	combinedMap := make(map[string]*uint)
	for k := range flagstovalues {
		subflags := flagstovalues[k]
		for k, v := range subflags {
			combinedMap[k] = v
		}
	}
	for k := range combinedMap {
		if theint, err2 := context.Int(k); err2 == nil {
			*combinedMap[k] = uint(theint)
		}
	}
}

func CastConfigDurations(context altsrc.InputSourceContext, flagstovalues []map[string]*time.Duration) {
	combinedMap := make(map[string]*time.Duration)
	for k := range flagstovalues {
		subflags := flagstovalues[k]
		for k2, v := range subflags {
			combinedMap[k2] = v
		}
	}
	for k := range combinedMap {
		if durationString, err2 := context.String(k); err2 == nil {
			if dur, err := time.ParseDuration(durationString); err == nil {
				*combinedMap[k] = dur
			}
		}
	}
}

