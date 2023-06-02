// Package cmd is the base package for various sets of builds and executables created from go-spacemesh
package cmd

import (
	"math/big"
	"reflect"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/node/flags"
)

var (
	// Version is the app's semantic version. Designed to be overwritten by make.
	Version string

	// Branch is the git branch used to build the App. Designed to be overwritten by make.
	Branch string

	// Commit is the git commit used to build the app. Designed to be overwritten by make.
	Commit string
)

// EnsureCLIFlags checks flag types and converts them.
func EnsureCLIFlags(cmd *cobra.Command, appCFG *config.Config) error {
	assignFields := func(p reflect.Type, elem reflect.Value, name string) {
		for i := 0; i < p.NumField(); i++ {
			if p.Field(i).Tag.Get("mapstructure") == name {
				var val any
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

			ff = reflect.TypeOf(appCFG.Bootstrap)
			elem = reflect.ValueOf(&appCFG.Bootstrap).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appCFG.Recovery)
			elem = reflect.ValueOf(&appCFG.Recovery).Elem()
			assignFields(ff, elem, name)
		}
	})
	return nil
}
