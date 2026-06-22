package live

import (
	"encoding/json"
	"math/big"

	"github.com/Chaintable/pipeline/tracer"
	"github.com/Chaintable/pipeline/util"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/params"
)

// 需要上传3种data
// 1. block
// 2. state diff
// 3. block file

func init() {
	tracers.LiveDirectory.Register("pipeline", NewPipelineTracer)
}

type pipelineTracerConfig struct {
	Region               string   `json:"region"`
	NodeXBucket          string   `json:"node_x_bucket"`
	ChainTableBucket     string   `json:"chain_table_bucket"`
	Brokers              []string `json:"brokers"`
	Topic                string   `json:"topic"`
	S3TempDir            string   `json:"s3_temp_dir"`
	IsBackup             *bool    `json:"is_backup"` // nil = auto (use etcd), false = leader in fixed mode, true = backup in fixed mode
	EnablePreStateTracer bool     `json:"enable_prestate_tracer"`

	// Auto failover configurations
	EtcdEndpoints []string `json:"etcd_endpoints"`
	ElectionKey   string   `json:"election_key"`
	NodeID        string   `json:"node_id"`      // default to hostname
	GracePeriod   int      `json:"grace_period"` // default to 10 seconds, unit is second

	// Writer node registry configurations
	WriterRegistryTTL int64 `json:"writer_registry_ttl"` // TTL for writer node registration in seconds, default 30
}

type NativePipelineTracer struct {
	tracer *tracer.PipelineTracer
}

func NewNativePipelineTracer(cfg json.RawMessage) (*NativePipelineTracer, error) {
	pipelineTracer, err := tracer.NewPipelineTracer(cfg)
	if err != nil {
		return nil, err
	}
	return &NativePipelineTracer{
		tracer: pipelineTracer,
	}, nil
}

func (t *NativePipelineTracer) OnBlockchainInit(chainConfig *params.ChainConfig) {
	if t.tracer != nil {
		t.tracer.OnBlockchainInit(chainConfig)
	}
}

func (t *NativePipelineTracer) OnClose() {
	if t.tracer != nil {
		t.tracer.OnClose()
	}
}

func (t *NativePipelineTracer) OnBlockStart(event tracing.BlockEvent) {
	if t.tracer != nil {
		t.tracer.OnBlockStart(event)
	}
}

func (t *NativePipelineTracer) OnTxStart(vm *tracing.VMContext, tx *types.Transaction, from common.Address) {
	if t.tracer != nil {
		t.tracer.OnTxStart(vm, tx, from)
	}
}

func (t *NativePipelineTracer) OnTxEnd(receipt *types.Receipt, err error) {
	if t.tracer != nil {
		t.tracer.OnTxEnd(receipt, err)
	}
}

func (t *NativePipelineTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
	if t.tracer != nil {
		t.tracer.OnEnter(depth, typ, from, to, input, gas, value)
	}
}

func (t *NativePipelineTracer) OnExit(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
	if t.tracer != nil {
		t.tracer.OnExit(depth, output, gasUsed, err, reverted)
	}
}

func (t *NativePipelineTracer) OnLog(log *types.Log) {
	if t.tracer != nil {
		t.tracer.OnLog(log)
	}
}

func (t *NativePipelineTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	if t.tracer != nil {
		t.tracer.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
	}
}

func (t *NativePipelineTracer) OnGenesisBlock(block *types.Block, alloc types.GenesisAlloc) {
	if t.tracer != nil {
		t.tracer.OnGenesisBlock(block, alloc)
	}
}

func (t *NativePipelineTracer) OnCommit(originRoot common.Hash, root common.Hash, destructs map[common.Hash]struct{}, accounts map[common.Hash][]byte, accountsOrigin map[common.Address][]byte, storages map[common.Hash]map[common.Hash][]byte, storagesOrigin map[common.Address]map[common.Hash][]byte, codes map[common.Hash][]byte) {
	if t.tracer != nil {
		t.tracer.OnCommit(originRoot, root, destructs, accounts, accountsOrigin, storages, storagesOrigin, codes)
	}
}

func (t *NativePipelineTracer) OnBlockFix() {}

func (t *NativePipelineTracer) OnBlockEnd(err error) {
	if t.tracer != nil {
		t.tracer.OnBlockEnd(err)
	}
}

func (t *NativePipelineTracer) OnBlockValidated(block *types.Block) {
	if t.tracer != nil {
		if block != nil {
			tracer.BlockCtx.BlockHash = block.Hash()
			tracer.BlockCtx.BlockHeader = util.BuildPilelineBlockHeader(block)
			tracer.BlockCtx.BlockFile.Block = util.BuildPipelineBlock(block)
		}
	}
}

func NewPipelineTracer(cfg json.RawMessage) (*tracing.Hooks, error) {
	t, err := NewNativePipelineTracer(cfg)
	if err != nil {
		return nil, err
	}
	return &tracing.Hooks{
		OnBlockchainInit: t.OnBlockchainInit,
		OnClose:          t.OnClose,
		OnBlockStart:     t.OnBlockStart,
		OnTxStart:        t.OnTxStart,
		OnTxEnd:          t.OnTxEnd,
		OnEnter:          t.OnEnter,
		OnExit:           t.OnExit,
		OnLog:            t.OnLog,
		OnOpcode:         t.OnOpcode,
		OnGenesisBlock:   t.OnGenesisBlock,
		OnCommit:         t.OnCommit,
		OnBlockValidated: t.OnBlockValidated,
	}, nil
}
