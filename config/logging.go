package config

import "go.uber.org/zap/zapcore"

type LogEncoder = string

const (
	PlainTextLogEncoder LogEncoder = "plain"
	JSONLogEncoder      LogEncoder = "json"
	defaultLoggingLevel            = zapcore.InfoLevel
)

// LoggerConfig holds the logging level for each module.
type LoggerConfig struct {
	Encoder                   LogEncoder `mapstructure:"log-encoder"`
	AppLoggerLevel            string     `mapstructure:"app"`
	P2PLoggerLevel            string     `mapstructure:"p2p"`
	PostLoggerLevel           string     `mapstructure:"post"`
	StateDbLoggerLevel        string     `mapstructure:"stateDb"`
	StateLoggerLevel          string     `mapstructure:"state"`
	AtxDbStoreLoggerLevel     string     `mapstructure:"atxDbStore"`
	TBeaconLoggerLevel        string     `mapstructure:"tBeacon"`
	WeakCoinLoggerLevel       string     `mapstructure:"weakCoin"`
	PoetDbStoreLoggerLevel    string     `mapstructure:"poetDbStore"`
	StoreLoggerLevel          string     `mapstructure:"store"`
	PoetDbLoggerLevel         string     `mapstructure:"poetDb"`
	MeshDBLoggerLevel         string     `mapstructure:"meshDb"`
	TrtlLoggerLevel           string     `mapstructure:"trtl"`
	AtxDbLoggerLevel          string     `mapstructure:"atxDb"`
	BlkEligibilityLoggerLevel string     `mapstructure:"block-eligibility"`
	MeshLoggerLevel           string     `mapstructure:"mesh"`
	SyncLoggerLevel           string     `mapstructure:"sync"`
	BlockOracleLevel          string     `mapstructure:"block-oracle"`
	HareOracleLoggerLevel     string     `mapstructure:"hare-oracle"`
	HareLoggerLevel           string     `mapstructure:"hare"`
	BlockBuilderLoggerLevel   string     `mapstructure:"block-builder"`
	BlockListenerLoggerLevel  string     `mapstructure:"block-listener"`
	PoetListenerLoggerLevel   string     `mapstructure:"poet"`
	NipostBuilderLoggerLevel  string     `mapstructure:"nipost"`
	AtxBuilderLoggerLevel     string     `mapstructure:"atx-builder"`
	HareBeaconLoggerLevel     string     `mapstructure:"hare-beacon"`
	TimeSyncLoggerLevel       string     `mapstructure:"timesync"`
}

func defaultLoggingConfig() LoggerConfig {
	return LoggerConfig{
		Encoder:                   PlainTextLogEncoder,
		AppLoggerLevel:            defaultLoggingLevel.String(),
		P2PLoggerLevel:            defaultLoggingLevel.String(),
		PostLoggerLevel:           defaultLoggingLevel.String(),
		StateDbLoggerLevel:        defaultLoggingLevel.String(),
		StateLoggerLevel:          defaultLoggingLevel.String(),
		AtxDbStoreLoggerLevel:     defaultLoggingLevel.String(),
		TBeaconLoggerLevel:        defaultLoggingLevel.String(),
		WeakCoinLoggerLevel:       defaultLoggingLevel.String(),
		PoetDbStoreLoggerLevel:    defaultLoggingLevel.String(),
		StoreLoggerLevel:          defaultLoggingLevel.String(),
		PoetDbLoggerLevel:         defaultLoggingLevel.String(),
		MeshDBLoggerLevel:         defaultLoggingLevel.String(),
		TrtlLoggerLevel:           defaultLoggingLevel.String(),
		AtxDbLoggerLevel:          defaultLoggingLevel.String(),
		BlkEligibilityLoggerLevel: defaultLoggingLevel.String(),
		MeshLoggerLevel:           defaultLoggingLevel.String(),
		SyncLoggerLevel:           defaultLoggingLevel.String(),
		BlockOracleLevel:          defaultLoggingLevel.String(),
		HareOracleLoggerLevel:     defaultLoggingLevel.String(),
		HareLoggerLevel:           defaultLoggingLevel.String(),
		BlockBuilderLoggerLevel:   defaultLoggingLevel.String(),
		BlockListenerLoggerLevel:  defaultLoggingLevel.String(),
		PoetListenerLoggerLevel:   defaultLoggingLevel.String(),
		NipostBuilderLoggerLevel:  defaultLoggingLevel.String(),
		AtxBuilderLoggerLevel:     defaultLoggingLevel.String(),
		HareBeaconLoggerLevel:     defaultLoggingLevel.String(),
		TimeSyncLoggerLevel:       defaultLoggingLevel.String(),
	}
}
