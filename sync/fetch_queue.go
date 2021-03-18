package sync

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"runtime"
	"runtime/debug"
	"sync"
)

type fetchPoetProofFunc func(ctx context.Context, poetProofRef []byte) error
type sValidateAtxFunc func(atx *types.ActivationTx) error
type sFetchAtxFunc func(atx *types.ActivationTx) error
type checkLocalFunc func(ids []types.Hash32) (map[types.Hash32]item, map[types.Hash32]item, []types.Hash32)

type item interface {
	Hash32() types.Hash32
	ShortString() string
}

type fetchJob struct {
	items []item
	ids   []types.Hash32
}

//todo make the queue generic
type fetchQueue struct {
	log.Log
	batchRequestFactory
	*sync.Mutex
	workerInfra networker
	pending     map[types.Hash32][]chan bool
	handleFetch func(context.Context, fetchJob)
	checkLocal  checkLocalFunc
	queue       chan []types.Hash32 //types.TransactionID //todo make buffered
	name        string
}

func (fq *fetchQueue) Close() {
	close(fq.queue)
	fq.Info("done")
}

func concatShortIds(items []types.Hash32) string {
	str := ""
	for i, h := range items {
		str += h.ShortString()
		if i < len(items)-1 {
			str += " "
		}
	}
	return str
}

func (fq *fetchQueue) shutdownRecover() {
	r := recover()
	if r != nil {
		fq.Info("%s shut down ", fq.name)
		fq.Error("stacktrace from panic: \n" + string(debug.Stack()))
	}
}

//todo batches
func (fq *fetchQueue) work(ctx context.Context) {
	logger := fq.WithContext(ctx)
	defer fq.shutdownRecover()
	parallelWorkers := runtime.NumCPU()
	output := fetchWithFactory(newFetchWorker(ctx, fq.workerInfra, runtime.NumCPU(), fq.batchRequestFactory, fq.queue, fq.name))
	wg := sync.WaitGroup{}
	wg.Add(parallelWorkers)
	for i := 0; i < parallelWorkers; i++ {
		go func() {
			logger.Info("running work")
			for out := range output {
				logger.Info("new batch out of queue")
				if out == nil {
					logger.Info("close queue")
					break
				}

				bjb, ok := out.(fetchJob)
				if !ok {
					fq.Error(fmt.Sprintf("Type assertion err %v", out))
					continue
				}

				if len(bjb.ids) == 0 {
					break //fmt.Errorf("channel closed")
				}

				logger.With().Info("attempting to fetch objects",
					log.String("type", fq.name),
					log.String("ids", concatShortIds(bjb.ids)))
				fq.handleFetch(ctx, bjb)
				logger.Info("done fetching, going to next batch")
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func (fq *fetchQueue) addToPendingGetCh(ids []types.Hash32) chan bool {
	return getDoneChan(fq.addToPending(ids))
}

func (fq *fetchQueue) addToPending(ids []types.Hash32) []chan bool {
	//defer fq.shutdownRecover()
	fq.Lock()
	deps := make([]chan bool, 0, len(ids))
	for _, id := range ids {
		ch := make(chan bool, 1)
		deps = append(deps, ch)
		fq.pending[id] = append(fq.pending[id], ch)
	}
	fq.Unlock()
	if len(ids) > 0 {
		fq.queue <- ids
	}
	return deps
}

func (fq *fetchQueue) invalidate(id types.Hash32, valid bool) {
	fq.Lock()
	deps := fq.pending[id]
	delete(fq.pending, id)
	fq.Unlock()

	for _, dep := range deps {
		dep <- valid
	}
	fq.Debug("invalidated %v %v", id.ShortString(), valid)
}

//returns items out of itemIds that are not in the local database
func (fq *fetchQueue) handle(itemIds []types.Hash32) (map[types.Hash32]item, error) {
	if len(itemIds) == 0 {
		fq.Debug("handle empty item ids slice")
		return nil, nil
	}

	unprocessedItems, _, missing := fq.checkLocal(itemIds)
	if len(missing) > 0 {

		output := fq.addToPendingGetCh(missing)

		if success := <-output; !success {
			return nil, fmt.Errorf("could not fetch all items")
		}

		unprocessedItems, _, missing = fq.checkLocal(itemIds)
		if len(missing) > 0 {
			return nil, errors.New("could not find all items even though fetch was successful")
		}
	}

	return unprocessedItems, nil
}

//todo make the queue generic
type txQueue struct {
	fetchQueue
}

func newTxQueue(ctx context.Context, s *Syncer) *txQueue {
	//todo buffersize
	q := &txQueue{
		fetchQueue: fetchQueue{
			Log:                 s.Log.WithName("txFetchQueue"),
			workerInfra:         s.net,
			Mutex:               &sync.Mutex{},
			batchRequestFactory: txFetchReqFactory,
			checkLocal:          s.txCheckLocal,
			pending:             make(map[types.Hash32][]chan bool),
			queue:               make(chan []types.Hash32, 10000),
			name:                "Tx",
		},
	}

	q.handleFetch = updateTxDependencies(q.invalidate, s.txpool)
	go q.work(log.WithNewSessionID(ctx))
	return q
}

//we could get rid of this if we had a unified id type
func (tx txQueue) HandleTxs(txids []types.TransactionID) ([]*types.Transaction, error) {
	txItems := make([]types.Hash32, 0, len(txids))
	for _, i := range txids {
		txItems = append(txItems, i.Hash32())
	}

	txres, err := tx.handle(txItems)
	if err != nil {
		return nil, err
	}

	txs := make([]*types.Transaction, 0, len(txres))
	for _, i := range txres {
		txs = append(txs, i.(*types.Transaction))
	}

	return txs, nil
}

func updateTxDependencies(invalidate func(id types.Hash32, valid bool), txpool txMemPool) func(context.Context, fetchJob) {
	return func(ctx context.Context, fj fetchJob) {
		mp := map[types.Hash32]*types.Transaction{}

		for _, item := range fj.items {
			mp[item.Hash32()] = item.(*types.Transaction)
		}

		for _, id := range fj.ids {
			if item, ok := mp[id]; ok {
				txpool.Put(types.TransactionID(id), item)
				invalidate(id, true)
			} else {
				invalidate(id, false)
			}
		}
	}
}

type atxQueue struct {
	fetchQueue
}

func newAtxQueue(ctx context.Context, s *Syncer, fetchPoetProof fetchPoetProofFunc) *atxQueue {
	//todo buffersize
	q := &atxQueue{
		fetchQueue: fetchQueue{
			Log:                 s.Log.WithName("atxFetchQueue"),
			workerInfra:         s.net,
			batchRequestFactory: atxFetchReqFactory,
			Mutex:               &sync.Mutex{},
			checkLocal:          s.atxCheckLocal,
			pending:             make(map[types.Hash32][]chan bool),
			queue:               make(chan []types.Hash32, 10000),
			name:                "Atx",
		},
	}

	q.handleFetch = updateAtxDependencies(ctx, q.invalidate, s.SyntacticallyValidateAtx, s.FetchAtxReferences, s.atxDb, fetchPoetProof, q.Log)
	go q.work(log.WithNewSessionID(ctx))
	return q
}

//we could get rid of this if we had a unified id type
func (atx atxQueue) HandleAtxs(atxids []types.ATXID) ([]*types.ActivationTx, error) {
	atx.Log.Debug("going to fetch %v", atxids)
	atxItems := make([]types.Hash32, 0, len(atxids))
	for _, i := range atxids {
		atxItems = append(atxItems, i.Hash32())
	}

	atxres, err := atx.handle(atxItems)
	if err != nil {
		atx.Log.Error("cannot fetch all atxs for block %v", err)
		return nil, err
	}

	atxs := make([]*types.ActivationTx, 0, len(atxres))
	for _, i := range atxres {
		atxs = append(atxs, i.(*types.ActivationTx))
	}

	return atxs, nil
}

func updateAtxDependencies(ctx context.Context, invalidate func(id types.Hash32, valid bool), sValidateAtx sValidateAtxFunc, fetchAtxRefs sFetchAtxFunc, atxDB atxDB, fetchProof fetchPoetProofFunc, logger log.Log) func(context.Context, fetchJob) {
	logger = logger.WithContext(ctx)
	return func(ctx context.Context, fj fetchJob) {
		fetchProofCalcID(ctx, logger, fetchProof, fj)

		mp := map[types.Hash32]*types.ActivationTx{}
		for _, item := range fj.items {
			tmp := item.(*types.ActivationTx)
			mp[item.Hash32()] = tmp
		}

		for _, id := range fj.ids {
			logger = logger.WithContext(ctx).WithFields(log.String("job_id", id.String()))
			if atx, ok := mp[id]; ok {
				logger.With().Info("atx queue work item", atx.ID())
				if err := fetchAtxRefs(atx); err != nil {
					logger.With().Warning("failed to fetch referenced atxs", log.Err(err))
					invalidate(id, false)
					continue
				}
				if err := sValidateAtx(atx); err != nil {
					logger.With().Warning("failed to validate atx", atx.ID(), log.Err(err))
					invalidate(id, false)
					continue
				}
				if err := atxDB.ProcessAtx(atx); err != nil {
					logger.Warning("failed to add atx to db", log.Err(err))
					invalidate(id, false)
					continue
				}
				logger.Info("atx queue work item ok", atx.ID())
				invalidate(id, true)
			} else {
				logger.Error("job returned with no response")
				invalidate(id, false)
			}
		}
	}
}

func getDoneChan(deps []chan bool) chan bool {
	doneChan := make(chan bool)
	//fan in
	go func() {
		alldone := true
		for _, c := range deps {
			if done := <-c; !done {
				alldone = false
				break
			}
		}
		doneChan <- alldone
		close(doneChan)
	}()
	return doneChan
}

func fetchProofCalcID(ctx context.Context, logger log.Log, fetchPoetProof fetchPoetProofFunc, fj fetchJob) {
	itemsWithProofs := make([]item, 0, len(fj.items))
	for _, item := range fj.items {
		atx := item.(*types.ActivationTx)
		atx.CalcAndSetID() //todo put it somewhere that will cause less confusion
		if err := fetchPoetProof(ctx, atx.GetPoetProofRef()); err != nil {
			logger.With().Error("received atx  with syntactically invalid or missing PoET proof",
				atx.ID(),
				log.String("poet_proof_ref", fmt.Sprint(atx.GetShortPoetProofRef())),
				log.Err(err))
			continue
		}
		itemsWithProofs = append(itemsWithProofs, atx)
	}

	fj.items = itemsWithProofs
}
