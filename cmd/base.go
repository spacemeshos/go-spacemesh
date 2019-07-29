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

var EntryPointCreated = make(chan bool, 1)

var (
	// Version is the app's semantic version. Designed to be overwritten by make.
	Version = "0.0.1"

	// Branch is the git branch used to build the App. Designed to be overwritten by make.
	Branch = ""

	// Commit is the git commit used to build the app. Designed to be overwritten by make.
	Commit = ""
	// ctx    cancel used to signal the app to gracefully exit.
	Ctx, Cancel = context.WithCancel(context.Background())
)

type BaseApp struct {
	Config *bc.Config
}

func NewBaseApp() *BaseApp {
	dc := bc.DefaultConfig()
	return &BaseApp{Config: &dc}
}

func (app *BaseApp) Initialize(cmd *cobra.Command) {
	// exit gracefully - e.g. with app Cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	// Goroutine that listens for Crtl ^ C command
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

	EnsureCLIFlags(cmd, app.Config)
	setupLogging(app.Config)
}

func setupLogging(config *bc.Config) {

	if config.TestMode {
		log.JSONLog(true)
	}

	// setup logging early
	dataDir, err := filesystem.GetSpacemeshDataDirectoryPath()
	if err != nil {
		fmt.Printf("Failed to setup spacemesh data dir")
		log.Panic("Failed to setup spacemesh data dir", err)
	}

	// app-level logging
	log.InitSpacemeshLoggingSystem(dataDir, "spacemesh.log")
}

func parseConfig() (*bc.Config, error) {
	fileLocation := viper.GetString("config")
	vip := viper.New()
	// read in default config if passed as param using viper
	if err := bc.LoadConfig(fileLocation, vip); err != nil {
		log.Error(fmt.Sprintf("couldn't load config file at location: %s swithing to defaults \n error: %v.",
			fileLocation, err))
		//return err
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

func EnsureCLIFlags(cmd *cobra.Command, appcfg *bc.Config) {
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

			ff := reflect.TypeOf(appcfg.BaseConfig)
			elem := reflect.ValueOf(&appcfg.BaseConfig).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(*appcfg)
			elem = reflect.ValueOf(&appcfg).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.API)
			elem = reflect.ValueOf(&appcfg.API).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.P2P)
			elem = reflect.ValueOf(&appcfg.P2P).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.P2P.SwarmConfig)
			elem = reflect.ValueOf(&appcfg.P2P.SwarmConfig).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.TIME)
			elem = reflect.ValueOf(&appcfg.TIME).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.HARE)
			elem = reflect.ValueOf(&appcfg.HARE).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.HareEligibility)
			elem = reflect.ValueOf(&appcfg.HareEligibility).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.POST)
			elem = reflect.ValueOf(&appcfg.POST).Elem()
			assignFields(ff, elem, name)
		}
	})
}
