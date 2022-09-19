// Package cmd is the base package for various sets of builds and executables created from go-spacemesh
package cmd

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"reflect"
	"sync"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/spacemeshos/go-spacemesh/cmd/flags"
	"github.com/spacemeshos/go-spacemesh/cmd/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/common/types"
	bc "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	// Version is the app's semantic version. Designed to be overwritten by make.
	Version string

	// Branch is the git branch used to build the App. Designed to be overwritten by make.
	Branch string

	// Commit is the git commit used to build the app. Designed to be overwritten by make.
	Commit string
)

var (
	mu                      sync.RWMutex
	globalCtx, globalCancel = context.WithCancel(context.Background())
)

// Ctx returns global context.
func Ctx() context.Context {
	mu.RLock()
	defer mu.RUnlock()

	return globalCtx
}

// SetCtx sets global context.
func SetCtx(ctx context.Context) {
	mu.Lock()
	defer mu.Unlock()

	globalCtx = ctx
}

// Cancel returns global cancellation function.
func Cancel() func() {
	mu.RLock()
	defer mu.RUnlock()

	return globalCancel
}

// SetCancel sets global cancellation function.
func SetCancel(cancelFunc func()) {
	mu.Lock()
	defer mu.Unlock()

	globalCancel = cancelFunc
}

// BaseApp is the base application command, provides basic init and flags for all executables and applications.
type BaseApp struct {
	Config *bc.Config
}

// NewBaseApp returns new basic application.
func NewBaseApp() *BaseApp {
	dc := bc.DefaultConfig()
	return &BaseApp{Config: &dc}
}

// Initialize loads config, sets logger  and listens to Ctrl ^C.
func (app *BaseApp) Initialize(cmd *cobra.Command) {
	// exit gracefully - e.g. with app Cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	// Goroutine that listens for Ctrl ^ C command
	// and triggers the quit app
	go func() {
		for range signalChan {
			log.Info("Received an interrupt, stopping services...\n")

			Cancel()()
		}
	}()

	// parse the config file based on flags et al
	conf, err := parseConfig()
	if err != nil {
		log.Panic("Panic: ", err.Error())
	}

	app.Config = conf
	if err := EnsureCLIFlags(cmd, app.Config); err != nil {
		log.Panic(err.Error())
	}
	setupLogging(app.Config)
}

func setupLogging(config *bc.Config) {
	if config.LOGGING.Encoder == bc.JSONLogEncoder {
		log.JSONLog(true)
	}

	// setup logging early
	err := filesystem.ExistOrCreate(config.DataDir())
	if err != nil {
		log.Panic("Failed to setup spacemesh data dir", err)
	}
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
	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructureutil.BigRatDecodeFunc(),
	)

	// load config if it was loaded to our viper
	err := vip.Unmarshal(&conf, viper.DecodeHook(hook))
	if err != nil {
		log.Error("Failed to parse config\n")
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &conf, nil
}

// EnsureCLIFlags checks flag types and converts them.
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
				case "int":
					val = viper.GetInt(name)
				case "int8":
					val = int8(viper.GetInt(name))
				case "int16":
					val = int16(viper.GetInt(name))
				case "int32":
					val = viper.GetInt32(name)
				case "int64":
					val = viper.GetInt64(name)
				case "uint":
					val = viper.GetUint(name)
				case "uint8":
					val = uint8(viper.GetUint(name))
				case "uint16":
					val = uint16(viper.GetUint(name))
				case "uint32":
					val = viper.GetUint32(name)
				case "uint64":
					val = viper.GetUint64(name)
				case "float64":
					val = viper.GetFloat64(name)
				case "[]string":
					val = viper.GetStringSlice(name)
				case "time.Duration":
					val = viper.GetDuration(name)
				case "map[string]uint64":
					val = flags.CastStringToMapStringUint64(viper.GetString(name))
				case "*big.Rat":
					v, ok := new(big.Rat).SetString(viper.GetString(name))
					if !ok {
						panic("bad string for *big.Rat provided")
					}
					val = v
				case "types.RoundID":
					val = types.RoundID(viper.GetUint64(name))
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

			ff = reflect.TypeOf(*appCFG.Genesis)
			elem = reflect.ValueOf(appCFG.Genesis).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(*appCFG.Genesis.Accounts)
			elem = reflect.ValueOf(appCFG.Genesis.Accounts).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.API)
			elem = reflect.ValueOf(&appCFG.API).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.P2P)
			elem = reflect.ValueOf(&appCFG.P2P).Elem()
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

			ff = reflect.TypeOf(appCFG.Beacon)
			elem = reflect.ValueOf(&appCFG.Beacon).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.POET)
			elem = reflect.ValueOf(&appCFG.POET).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.POST)
			elem = reflect.ValueOf(&appCFG.POST).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.SMESHING)
			elem = reflect.ValueOf(&appCFG.SMESHING).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.SMESHING.Opts)
			elem = reflect.ValueOf(&appCFG.SMESHING.Opts).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.LOGGING)
			elem = reflect.ValueOf(&appCFG.LOGGING).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.Tortoise)
			elem = reflect.ValueOf(&appCFG.Tortoise).Elem()
			assignFields(ff, elem, name)
		}
	})
	// check list of requested GRPC services (if any)
	if err := appCFG.API.ParseServicesList(); err != nil {
		return fmt.Errorf("parse services list: %w", err)
	}

	return nil
}
