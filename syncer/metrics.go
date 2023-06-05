package syncer

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "syncer"
)

var (
	numRuns = metrics.NewCounter(
		"runs",
		namespace,
		"number of sync runs",
		[]string{"outcome"},
	)
	runSuccess = numRuns.WithLabelValues("ok")
	runFail    = numRuns.WithLabelValues("not")

	stateRuns = metrics.NewCounter(
		"state_runs",
		namespace,
		"number of state sync runs",
		[]string{"outcome"},
	)
	sRunSuccess = stateRuns.WithLabelValues("ok")
	sRunFail    = stateRuns.WithLabelValues("not")

	syncedLayers = metrics.NewGauge(
		"layers",
		namespace,
		"synced layers in different data type",
		[]string{"data"},
	)
	dataLayer    = syncedLayers.WithLabelValues("data")
	opinionLayer = syncedLayers.WithLabelValues("opinion")

	syncedEpochs = metrics.NewGauge(
		"epochs",
		namespace,
		"synced epochs in different data type",
		[]string{"data"},
	)
	atxEpoch = syncedEpochs.WithLabelValues("atx")

	nodeSyncState = metrics.NewGauge(
		"sync_state",
		namespace,
		"node sync state in [not_synced, gossip, synced]",
		[]string{"state"},
	)
	nodeNotSynced = nodeSyncState.WithLabelValues("not")
	nodeGossip    = nodeSyncState.WithLabelValues("gossip")
	nodeSynced    = nodeSyncState.WithLabelValues("synced")
	atxSynced     = nodeSyncState.WithLabelValues("atx_synced")

	numHashResolution = metrics.NewCounter(
		"hash_resolution",
		namespace,
		"number of hash resolution with peers",
		[]string{"outcome"},
	)
	hashResolve     = numHashResolution.WithLabelValues("ok")
	hashResolveFail = numHashResolution.WithLabelValues("not")

	numCertAdopted = metrics.NewCounter(
		"adopted_cert",
		namespace,
		"number of cert adopted",
		[]string{},
	).WithLabelValues()

	blockRequested = metrics.NewCounter(
		"block_requested",
		namespace,
		"number of missing block requested",
		[]string{},
	).WithLabelValues()

	syncedLayer = metrics.NewGauge(
		"layer",
		namespace,
		"synced layer",
		[]string{},
	).WithLabelValues()
)
