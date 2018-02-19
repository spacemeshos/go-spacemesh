package config

import (
	"github.com/spacemeshos/go-spacemesh/assert"
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/urfave/cli.v1/altsrc"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"sort"
	"testing"
	"time"
)

type TestApp struct {
	App          *cli.App
	T            *testing.T
	TestFilePath string
}

// test new app with config.toml file
var TempTestFile = `int-param = 5
string-param = "spacemeshos"
duration-param = "10h"
bool-param = true
`

type duration struct {
	string
}

func (d *duration) Duration(t *testing.T) (duration time.Duration) {
	dur, err := time.ParseDuration(d.string)
	if err != nil {
		t.Error("Could not parse duration string returning 0, error:", err)
	}
	return dur
}

type TestConfig struct {
	TestParam     bool
	TestLogFile   string
	IntParam      int
	StringParam   string
	DurationParam duration
	BoolParam     bool
}

var TestConfigValues = TestConfig{}

var (
	TestFlag = cli.BoolFlag{
		Name:        "test.v",
		Usage:       "the test param",
		Destination: &TestConfigValues.TestParam,
	}

	TestRunFlag = cli.BoolFlag{
		Name:        "test.run",
		Usage:       "the test run param",
		Destination: &TestConfigValues.TestParam,
	}

	TestLogFileFlag = cli.StringFlag{
		Name:        "test.testlogfile",
		Usage:       "The file to log to",
		Value:       TestConfigValues.TestLogFile,
		Destination: &TestConfigValues.TestLogFile,
	}

	IntParamFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "int-param",
		Usage:       "Test an int param",
		Value:       TestConfigValues.IntParam,
		Destination: &TestConfigValues.IntParam,
	})

	StringParamFlag = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "string-param",
		Usage:       "Test a string param",
		Value:       TestConfigValues.StringParam,
		Destination: &TestConfigValues.StringParam,
	})

	DurationParamFlag = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "duration-param",
		Usage:       "Test a duration flag",
		Value:       TestConfigValues.DurationParam.string,
		Destination: &TestConfigValues.DurationParam.string,
	})

	BoolParamFlag = altsrc.NewBoolFlag(cli.BoolFlag{
		Name:        "bool-param",
		Usage:       "Test a bool flag",
		Destination: &TestConfigValues.BoolParam,
	})
)

func (testApp *TestApp) createTempFile() {
	// Get execution path
	_, filename, _, ok := runtime.Caller(1)
	if ok {
		filepath := path.Join(path.Dir(filename), "/tmp.toml")
		// Write file to tmp file.
		filebin := []byte(TempTestFile)
		err := ioutil.WriteFile(filepath, filebin, 0644)
		if err != nil {
			testApp.T.Error("Could not write tmp file error:", err)
		} else {
			testApp.TestFilePath = filepath
		}
	} else {
		testApp.T.Error("Could not find execution path")
	}
}

func (testApp *TestApp) removeTempFile(ctx *cli.Context) error {
	err := os.Remove(testApp.TestFilePath)
	if err != nil {
		testApp.T.Error("Could not remove tmp config file error:", err)
	}
	return err
}

func (testApp *TestApp) before(ctx *cli.Context) error {
	testApp.createTempFile()
	err := altsrc.InitInputSourceWithContext(ctx.App.Flags, func(context *cli.Context) (altsrc.InputSourceContext, error) {
		toml, err := altsrc.NewTomlSourceFromFile(testApp.TestFilePath)
		return toml, err
	})(ctx)
	if err != nil {
		testApp.T.Error("Config file had an error:", err)
		return err
	}
	return nil
}

func (testApp *TestApp) testValuesLoaded(ctx *cli.Context) error {
	assert.Equal(testApp.T, TestConfigValues.IntParam, 5, "")
	assert.Equal(testApp.T, TestConfigValues.StringParam, "spacemeshos", "")
	durationVal, _ := time.ParseDuration("10h")
	assert.Equal(testApp.T, TestConfigValues.DurationParam.Duration(testApp.T), durationVal, "")
	assert.Equal(testApp.T, TestConfigValues.BoolParam, true, "")
	return nil
}

func TestConfigLoading(t *testing.T) {
	app := cli.NewApp()
	testApp := TestApp{
		App: app,
		T:   t,
	}
	var appFlags = []cli.Flag{
		TestFlag,
		TestRunFlag,
		TestLogFileFlag,
		IntParamFlag,
		StringParamFlag,
		DurationParamFlag,
		BoolParamFlag,
	}
	app.Flags = appFlags

	sort.Sort(cli.FlagsByName(app.Flags))

	// setup callbacks
	app.Before = testApp.before
	app.Action = testApp.testValuesLoaded
	app.After = testApp.removeTempFile

	if err := app.Run(os.Args); err != nil {
		t.Error("Could not run app error:", err)
	}

}
