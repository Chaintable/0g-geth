package tracers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/internal/ethapi"
	"github.com/ethereum/go-ethereum/rpc"
)

// TraceAPI is the collection of Ethereum APIs exposed over the tracing
// namespace.
type TraceAPI struct {
	b Backend
}

// NewDebugAPI creates a new instance of DebugAPI.
func NewTraceAPI(b Backend) *TraceAPI {
	return &TraceAPI{b: b}
}

type ParityTrace struct {
	// Do not change the ordering of these fields -- allows for easier comparison with other clients
	Action              interface{} `json:"action"` // Can be either CallTraceAction or CreateTraceAction
	BlockHash           common.Hash `json:"blockHash,omitempty"`
	BlockNumber         uint64      `json:"blockNumber,omitempty"`
	Error               string      `json:"error,omitempty"`
	Result              interface{} `json:"result"`
	Subtraces           int         `json:"subtraces"`
	TraceAddress        []int       `json:"traceAddress"`
	TransactionHash     common.Hash `json:"transactionHash,omitempty"`
	TransactionPosition uint64      `json:"transactionPosition,omitempty"`
	Type                string      `json:"type"`
}

// TraceBlock returns the structured logs created during the execution of EVM
// and returns them as a JSON object.
func (tapi *TraceAPI) Block(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) ([]*ParityTrace, error) {
	var block *types.Block
	var err error
	if blockNr, ok := blockNrOrHash.Number(); ok {
		block, err = tapi.b.BlockByNumber(ctx, blockNr)
		if err != nil {
			return nil, err
		}
	} else if blockHash, ok := blockNrOrHash.Hash(); ok {
		block, err = tapi.b.BlockByHash(ctx, blockHash)
		if err != nil {
			return nil, err
		}
	}

	if block == nil {
		return nil, fmt.Errorf("missing block %v", blockNrOrHash.String())
	}

	return tapi.traceBlock(ctx, block, nil)
}

func (tapi *TraceAPI) blockByNumberAndHash(ctx context.Context, number rpc.BlockNumber, hash common.Hash) (*types.Block, error) {
	block, err := tapi.b.BlockByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	if block.Hash() == hash {
		return block, nil
	}
	return tapi.b.BlockByHash(ctx, hash)
}

// traceBlock configures a new tracer according to the provided configuration, and
// executes all the transactions contained within. The return value will be one item
// per transaction, dependent on the requested tracer.
func (tapi *TraceAPI) traceBlock(ctx context.Context, block *types.Block, config *TraceConfig) ([]*ParityTrace, error) {
	if block.NumberU64() == 0 {
		return nil, errors.New("genesis is not traceable")
	}
	// Prepare base state
	parent, err := tapi.blockByNumberAndHash(ctx, rpc.BlockNumber(block.NumberU64()-1), block.ParentHash())
	if err != nil {
		return nil, err
	}
	reexec := defaultTraceReexec
	if config != nil && config.Reexec != nil {
		reexec = *config.Reexec
	}
	statedb, release, err := tapi.b.StateAtBlock(ctx, parent, reexec, nil, true, false)
	if err != nil {
		return nil, err
	}
	defer release()

	chainCfg := tapi.b.ChainConfig()
	blockCtx := core.NewEVMBlockContext(block.Header(), ethapi.NewChainContext(ctx, tapi.b), nil)
	evm := vm.NewEVM(blockCtx, statedb, chainCfg, vm.Config{})
	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		core.ProcessBeaconBlockRoot(*beaconRoot, evm)
	}
	if chainCfg.IsPrague(block.Number(), block.Time()) {
		core.ProcessParentBlockHash(block.ParentHash(), evm)
	}

	// Native tracers have low overhead
	var (
		txs       = block.Transactions()
		blockHash = block.Hash()
		signer    = types.MakeSigner(chainCfg, block.Number(), block.Time())
		results   = []*ParityTrace{}
	)
	for i, tx := range txs {
		// Generate the next state snapshot fast without tracing
		msg, _ := core.TransactionToMessage(tx, signer, block.BaseFee())
		txctx := &Context{
			BlockHash:   blockHash,
			BlockNumber: block.Number(),
			TxIndex:     i,
			TxHash:      tx.Hash(),
		}
		res, err := tapi.traceTx(ctx, tx, msg, txctx, blockCtx, statedb, config, nil)
		if err != nil {
			return nil, err
		}

		for _, trace := range res.Trace {
			trace.BlockHash = txctx.BlockHash
			trace.BlockNumber = txctx.BlockNumber.Uint64()
			trace.TransactionHash = txctx.TxHash
			trace.TransactionPosition = uint64(txctx.TxIndex)
		}

		results = append(results, res.Trace...)
	}
	return results, nil
}

// TraceTransaction returns the structured logs created during the execution of EVM
// and returns them as a JSON object.
func (tapi *TraceAPI) Transaction(ctx context.Context, hash common.Hash) ([]*ParityTrace, error) {
	// try to find the transaction by hash
	found, _, blockHash, blockNumber, index := tapi.b.GetTransaction(hash)
	if !found {
		// Warn in case tx indexer is not done.
		if !tapi.b.TxIndexDone() {
			return nil, ethapi.NewTxIndexingError()
		}
		// Only mined txes are supported
		return nil, errTxNotFound
	}

	// try to find the block of the transaction
	// It shouldn't happen in practice.
	if blockNumber == 0 {
		return nil, errors.New("genesis is not traceable")
	}
	block, err := tapi.blockByNumberAndHash(ctx, rpc.BlockNumber(blockNumber), blockHash)
	if err != nil {
		return nil, err
	}

	// rebuild the execution environment of this transaction.
	tx, vmctx, statedb, release, err := tapi.b.StateAtTransaction(ctx, block, int(index), defaultTraceReexec)
	if err != nil {
		return nil, err
	}
	defer release()

	// convert the transaction to a message
	msg, err := core.TransactionToMessage(tx, types.MakeSigner(tapi.b.ChainConfig(), block.Number(), block.Time()), block.BaseFee())
	if err != nil {
		return nil, err
	}

	txctx := &Context{
		BlockHash:   blockHash,
		BlockNumber: block.Number(),
		TxIndex:     int(index),
		TxHash:      hash,
	}

	// trace the transaction
	result, err := tapi.traceTx(ctx, tx, msg, txctx, vmctx, statedb, nil, nil)
	if err != nil {
		return nil, err
	}

	return []*ParityTrace{
		&ParityTrace{
			// Action:,
			BlockHash:   block.Hash(),
			BlockNumber: block.NumberU64(),
			// Error: ,
			Result: result,
			// Subtraces: ,
			TraceAddress:        []int{},
			TransactionHash:     hash,
			TransactionPosition: index,
			// Type:,
		}}, nil
}

// type ParityTrace struct {
// 	// Do not change the ordering of these fields -- allows for easier comparison with other clients
// 	Action              interface{} `json:"action"` // Can be either CallTraceAction or CreateTraceAction
// 	BlockHash           common.Hash `json:"blockHash,omitempty"`
// 	BlockNumber         uint64      `json:"blockNumber,omitempty"`
// 	Error               string      `json:"error,omitempty"`
// 	Result              interface{} `json:"result"`
// 	Subtraces           int         `json:"subtraces"`
// 	TraceAddress        []int       `json:"traceAddress"`
// 	TransactionHash     common.Hash `json:"transactionHash,omitempty"`
// 	TransactionPosition uint64      `json:"transactionPosition,omitempty"`
// 	Type                string      `json:"type"`
// }

// traceTx configures a new tracer according to the provided configuration, and
// executes the given message in the provided environment. The return value will
// be tracer dependent.
func (tapi *TraceAPI) traceTx(
	ctx context.Context,
	tx *types.Transaction,
	message *core.Message,
	txctx *Context,
	vmctx vm.BlockContext,
	statedb *state.StateDB,
	config *TraceConfig,
	precompiles vm.PrecompiledContracts) (*TraceCallResult, error) {
	var (
		tracer  *Tracer
		err     error
		timeout = defaultTraceTimeout
		usedGas uint64
	)
	if config == nil {
		config = &TraceConfig{}
	}

	traceResult := &TraceCallResult{
		TransactionHash: txctx.TxHash,
		Trace:           []*ParityTrace{},
	}
	ttt := NewTxTraceTracer(config.Config)
	ttt.callResult = traceResult
	ttt.idx = []string{fmt.Sprintf("%d-", txctx.TxIndex)}
	tracer = ttt.Tracer()

	tracingStateDB := state.NewHookedState(statedb, tracer.Hooks)
	evm := vm.NewEVM(vmctx, tracingStateDB, tapi.b.ChainConfig(), vm.Config{Tracer: tracer.Hooks})
	if precompiles != nil {
		evm.SetPrecompiles(precompiles)
	}

	// Define a meaningful timeout of a single transaction trace
	if config.Timeout != nil {
		if timeout, err = time.ParseDuration(*config.Timeout); err != nil {
			return nil, err
		}
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	go func() {
		<-deadlineCtx.Done()
		if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
			tracer.Stop(errors.New("execution timeout"))
			// Stop evm execution. Note cancellation is not necessarily immediate.
			evm.Cancel()
		}
	}()
	defer cancel()

	// Call Prepare to clear out the statedb access list
	statedb.SetTxContext(txctx.TxHash, txctx.TxIndex)

	// Apply the transaction to the current state (included in the env).
	gp := new(core.GasPool).AddGas(message.GasLimit)
	execResult, err := core.ApplyMessage(evm, message, gp)
	if err != nil {
		return nil, fmt.Errorf("tracing failed: %w", err)
	}
	// Update the state with pending changes.
	// var postState []byte
	// if evm.ChainConfig().IsByzantium(vmctx.BlockNumber) {
	// 	evm.StateDB.Finalise(true)
	// } else {
	// 	postState = statedb.IntermediateRoot(evm.ChainConfig().IsEIP158(vmctx.BlockNumber)).Bytes()
	// }
	usedGas += execResult.UsedGas

	// Merge the tx-local access event into the "block-local" one, in order to collect
	// all values, so that the witness can be built.
	if statedb.GetTrie().IsVerkle() {
		statedb.AccessEvents().Merge(evm.AccessEvents)
	}

	traceResult.Output = common.CopyBytes(execResult.ReturnData)

	return traceResult, nil
}
