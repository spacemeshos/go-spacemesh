package types

type GenesisId Hash20

type GenesisTime string

type GoldenATXID Hash20

type GenesisData struct {
	genesis_id   GenesisId
	genesis_time GenesisTime
	golden_atx   GoldenATXID
}
