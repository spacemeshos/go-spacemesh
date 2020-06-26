// Package cmd is the base package for various sets of builds and executables created from go-spacemesh
package cmd

import (
	"context"
	"fmt"
	bc "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"os"
	"os/signal"
	"reflect"
)

var (
	// Version is the app's semantic version. Designed to be overwritten by make.
	Version string

	// Branch is the git branch used to build the App. Designed to be overwritten by make.
	Branch string

	// Commit is the git commit used to build the app. Designed to be overwritten by make.
	Commit string

	// Ctx is the node's main context.
	Ctx, cancel = context.WithCancel(context.Background())

	// Cancel is a function used to initiate graceful shutdown.
	Cancel = cancel
)

// BaseApp is the base application command, provides basic init and flags for all executables and applications
type BaseApp struct {
	Config *bc.Config
}

// NewBaseApp returns new basic application
func NewBaseApp() *BaseApp {
	dc := bc.DefaultConfig()
	return &BaseApp{Config: &dc}
}

// Initialize loads config, sets logger  and listens to Ctrl ^C
func (app *BaseApp) Initialize(cmd *cobra.Command) {
	// exit gracefully - e.g. with app Cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	// Goroutine that listens for Ctrl ^ C command
	// and triggers the quit app
	go func() {
		for range signalChan {
			log.Info("Received an interrupt, stopping services...\n")
			Cancel()
		}
	}()

	// parse the config file based on flags et al
	conf, err := parseConfig()
	if err != nil {
		log.Panic("Panic: ", err.Error())
	}

	app.Config = conf
	if err := EnsureCLIFlags(cmd, app.Config); err != nil {
		log.Panic("Panic: ", err.Error())
	}
	setupLogging(app.Config)
}

func setupLogging(config *bc.Config) {
	if config.TestMode {
		log.JSONLog(true)
	}

	// setup logging early
	err := filesystem.ExistOrCreate(config.DataDir())
	if err != nil {
		fmt.Printf("Failed to setup spacemesh data dir")
		log.Panic("Failed to setup spacemesh data dir", err)
	}

	// app-level logging
	log.InitSpacemeshLoggingSystem(config.DataDir(), "spacemesh.log")
}

func parseConfig() (*bc.Config, error) {
	fileLocation := viper.GetString("config")
	vip := viper.New()
	// read in default config if passed as param using viper
	if err := bc.LoadConfig(fileLocation, vip); err != nil {
		log.Error(fmt.Sprintf("couldn't load config file at location: %s switching to defaults \n error: %v.",
			fileLocation, err))
		// return err
	}

	conf := bc.DefaultConfig()
	// load config if it was loaded to our viper
	err := vip.Unmarshal(&conf)
	if err != nil {
		log.Error("Failed to parse config\n")
		return nil, err
	}

	return &conf, nil
}

// EnsureCLIFlags checks flag types and converts them
func EnsureCLIFlags(cmd *cobra.Command, appCFG *bc.Config) error {
	assignFields := func(p reflect.Type, elem reflect.Value, name string) {
		for i := 0; i < p.NumField(); i++ {
			if p.Field(i).Tag.Get("mapstructure") == name {
				var val interface{}
				switch p.Field(i).Type.String() {
				case "bool":
					val = viper.GetBool(name)
				case "string":
					val = viper.GetString(name)
				case "int", "int8", "int16":
					val = viper.GetInt(name)
				case "int32":
					val = viper.GetInt32(name)
				case "int64":
					val = viper.GetInt64(name)
				case "uint", "uint8", "uint16":
					val = viper.GetUint(name)
				case "uint32":
					val = viper.GetUint32(name)
				case "uint64":
					val = viper.GetUint64(name)
				case "float64":
					val = viper.GetFloat64(name)
				case "[]string":
					val = viper.GetStringSlice(name)
				default:
					val = viper.Get(name)
				}

				elem.Field(i).Set(reflect.ValueOf(val))
				return
			}
		}
	}

	// this is ugly but we have to do this because viper can't handle nested structs when deserialize
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			name := f.Name

			ff := reflect.TypeOf(appCFG.BaseConfig)
			elem := reflect.ValueOf(&appCFG.BaseConfig).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(*appCFG)
			elem = reflect.ValueOf(&appCFG).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.API)
			elem = reflect.ValueOf(&appCFG.API).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.P2P)
			elem = reflect.ValueOf(&appCFG.P2P).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.P2P.SwarmConfig)
			elem = reflect.ValueOf(&appCFG.P2P.SwarmConfig).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.TIME)
			elem = reflect.ValueOf(&appCFG.TIME).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.HARE)
			elem = reflect.ValueOf(&appCFG.HARE).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.HareEligibility)
			elem = reflect.ValueOf(&appCFG.HareEligibility).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.POST)
			elem = reflect.ValueOf(&appCFG.POST).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.LOGGING)
			elem = reflect.ValueOf(&appCFG.LOGGING).Elem()
			assignFields(ff, elem, name)
		}
	})
	// check list of requested GRPC services (if any)
	return appCFG.API.ParseServicesList()
}
