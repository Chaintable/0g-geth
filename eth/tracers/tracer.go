package tracers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/log"
)

const (
	CALL               = "call"
	CALLCODE           = "callcode"
	DELEGATECALL       = "delegatecall"
	STATICCALL         = "staticcall"
	CREATE             = "create"
	SUICIDE            = "suicide"
	REWARD             = "reward"
	TraceTypeTrace     = "trace"
	TraceTypeStateDiff = "stateDiff"
	TraceTypeVmTrace   = "vmTrace"
)

// TraceAction A parity formatted trace action
type TraceAction struct {
	// Do not change the ordering of these fields -- allows for easier comparison with other clients
	Author         string         `json:"author,omitempty"`
	RewardType     string         `json:"rewardType,omitempty"`
	SelfDestructed string         `json:"address,omitempty"`
	Balance        string         `json:"balance,omitempty"`
	CallType       string         `json:"callType,omitempty"`
	From           common.Address `json:"from"`
	Gas            hexutil.Big    `json:"gas"`
	Init           hexutil.Bytes  `json:"init,omitempty"`
	Input          hexutil.Bytes  `json:"input,omitempty"`
	RefundAddress  string         `json:"refundAddress,omitempty"`
	To             string         `json:"to,omitempty"`
	Value          string         `json:"value,omitempty"`
}

type CallTraceAction struct {
	From     common.Address `json:"from"`
	CallType string         `json:"callType"`
	Gas      hexutil.Big    `json:"gas"`
	Input    hexutil.Bytes  `json:"input"`
	To       common.Address `json:"to"`
	Value    hexutil.Big    `json:"value"`
}

type CreateTraceAction struct {
	From           common.Address `json:"from"`
	CreationMethod string         `json:"creationMethod"`
	Gas            hexutil.Big    `json:"gas"`
	Init           hexutil.Bytes  `json:"init"`
	Value          hexutil.Big    `json:"value"`
}

type SuicideTraceAction struct {
	Address       common.Address `json:"address"`
	RefundAddress common.Address `json:"refundAddress"`
	Balance       hexutil.Big    `json:"balance"`
}

type RewardTraceAction struct {
	Author     common.Address `json:"author"`
	RewardType string         `json:"rewardType"`
	Value      hexutil.Big    `json:"value,omitempty"`
}

type CreateTraceResult struct {
	// Do not change the ordering of these fields -- allows for easier comparison with other clients
	Address *common.Address `json:"address,omitempty"`
	Code    hexutil.Bytes   `json:"code"`
	GasUsed *hexutil.Big    `json:"gasUsed"`
}

type TraceResult struct {
	// Do not change the ordering of these fields -- allows for easier comparison with other clients
	GasUsed *hexutil.Big  `json:"gasUsed"`
	Output  hexutil.Bytes `json:"output"`
}

type VmTraceMem struct {
	Data string `json:"data"`
	Off  int    `json:"off"`
}

type VmTraceStore struct {
	Key string `json:"key"`
	Val string `json:"val"`
}

type VmTraceEx struct {
	Mem   *VmTraceMem   `json:"mem"`
	Push  []string      `json:"push"`
	Store *VmTraceStore `json:"store"`
	Used  int           `json:"used"`
}

type VmTraceOp struct {
	Cost int        `json:"cost"`
	Ex   *VmTraceEx `json:"ex"`
	Pc   int        `json:"pc"`
	Sub  *VmTrace   `json:"sub"`
	Op   string     `json:"op,omitempty"`
	Idx  string     `json:"idx,omitempty"`
}

type VmTrace struct {
	Ops []*VmTraceOp `json:"ops"`
}

type StateDiffAccount struct {
	Balance interface{}                            `json:"balance"` // Can be either string "=" or mapping "*" => {"from": "hex", "to": "hex"}
	Code    interface{}                            `json:"code"`
	Nonce   interface{}                            `json:"nonce"`
	Storage map[common.Hash]map[string]interface{} `json:"storage"`
}

type TraceCallResult struct {
	Output          hexutil.Bytes                        `json:"output"`
	StateDiff       map[common.Address]*StateDiffAccount `json:"stateDiff"`
	Trace           []*ParityTrace                       `json:"trace"`
	VmTrace         *VmTrace                             `json:"vmTrace"`
	TransactionHash common.Hash                          `json:"transactionHash,omitempty"`
}

type TxTracer struct {
	callResult   *TraceCallResult
	traceAddr    []int
	traceStack   []*ParityTrace
	lastVmOp     *VmTraceOp
	lastOp       vm.OpCode
	lastMemOff   uint64
	lastMemLen   uint64
	memOffStack  []uint64
	memLenStack  []uint64
	lastOffStack *VmTraceOp
	vmOpStack    []*VmTraceOp // Stack of vmTrace operations as call depth increases
	idx          []string     // Prefix for the "idx" inside operations, for easier navigation
}

func NewTxTraceTracer(cfg *logger.Config) *TxTracer {
	return &TxTracer{
		traceAddr: []int{},
	}
}

func (tt *TxTracer) OnEnter(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
	isCreate := vm.OpCode(typ) == vm.CREATE || vm.OpCode(typ) == vm.CREATE2
	deep := depth != 0
	thisType := vm.OpCode(typ)

	if tt.callResult.VmTrace != nil {
		if deep {
			var vmT *VmTrace
			if len(tt.vmOpStack) > 0 {
				vmT = tt.vmOpStack[len(tt.vmOpStack)-1].Sub
			} else {
				vmT = tt.callResult.VmTrace
			}

			tt.idx = append(tt.idx, fmt.Sprintf("%d-", len(vmT.Ops)-1))
		}
		if tt.lastVmOp != nil {
			tt.lastVmOp.Sub = &VmTrace{Ops: []*VmTraceOp{}}
			tt.vmOpStack = append(tt.vmOpStack, tt.lastVmOp)
		}
		if isCreate {
			if tt.lastVmOp != nil {
				tt.lastVmOp.Cost += int(gas)
			}
		}
	}

	if gas > 500000000 {
		gas = 500000001 - (0x8000000000000000 - gas)
	}
	trace := &ParityTrace{}
	if isCreate {
		trResult := &CreateTraceResult{}
		trace.Type = CREATE
		trResult.Address = new(common.Address)
		copy(trResult.Address[:], to.Bytes())
		trace.Result = trResult
	} else {
		trace.Result = &TraceResult{}
		trace.Type = CALL
	}
	if deep {
		topTrace := tt.traceStack[len(tt.traceStack)-1]
		traceIdx := topTrace.Subtraces
		tt.traceAddr = append(tt.traceAddr, traceIdx)
		topTrace.Subtraces++
		if thisType == vm.DELEGATECALL {
			switch action := topTrace.Action.(type) {
			case *CreateTraceAction:
				value = action.Value.ToInt()
			case *CallTraceAction:
				value = action.Value.ToInt()
			}
		}
		if thisType == vm.STATICCALL {
			value = big.NewInt(0)
		}
	}
	trace.TraceAddress = make([]int, len(tt.traceAddr))
	copy(trace.TraceAddress, tt.traceAddr)
	if isCreate {
		action := CreateTraceAction{}
		action.From = from
		action.CreationMethod = strings.ToLower(thisType.String())
		action.Gas.ToInt().SetUint64(gas)
		action.Init = common.CopyBytes(input)
		action.Value.ToInt().Set(value)
		trace.Action = &action
	} else if thisType == vm.SELFDESTRUCT {
		trace.Type = SUICIDE
		trace.Result = nil
		action := &SuicideTraceAction{}
		action.Address = from
		action.RefundAddress = to
		action.Balance.ToInt().Set(value)
		trace.Action = action
	} else {
		action := CallTraceAction{}
		switch thisType {
		case vm.CALL:
			action.CallType = CALL
		case vm.CALLCODE:
			action.CallType = CALLCODE
		case vm.DELEGATECALL:
			action.CallType = DELEGATECALL
		case vm.STATICCALL:
			action.CallType = STATICCALL
		}
		action.From = from
		action.To = to
		action.Gas.ToInt().SetUint64(gas)
		action.Input = common.CopyBytes(input)
		action.Value.ToInt().Set(value)
		trace.Action = &action
	}
	tt.callResult.Trace = append(tt.callResult.Trace, trace)
	tt.traceStack = append(tt.traceStack, trace)
}

func (tt *TxTracer) OnExit(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
	deep := depth != 0
	if tt.callResult.VmTrace != nil {
		if len(tt.vmOpStack) > 0 {
			tt.lastOffStack = tt.vmOpStack[len(tt.vmOpStack)-1]
			tt.vmOpStack = tt.vmOpStack[:len(tt.vmOpStack)-1]
		}
		if deep {
			tt.idx = tt.idx[:len(tt.idx)-1]
			tt.lastMemOff = tt.memOffStack[len(tt.memOffStack)-1]
			tt.memOffStack = tt.memOffStack[:len(tt.memOffStack)-1]
			tt.lastMemLen = tt.memLenStack[len(tt.memLenStack)-1]
			tt.memLenStack = tt.memLenStack[:len(tt.memLenStack)-1]
		}
	}

	if !deep {
		tt.callResult.Output = common.CopyBytes(output)
	}
	ignoreError := false
	topTrace := tt.traceStack[len(tt.traceStack)-1]

	if err != nil && !ignoreError {
		if errors.Is(err, vm.ErrExecutionReverted) {
			topTrace.Error = "Reverted"
			switch topTrace.Type {
			case CALL:
				topTrace.Result.(*TraceResult).GasUsed = new(hexutil.Big)
				topTrace.Result.(*TraceResult).GasUsed.ToInt().SetUint64(gasUsed)
				topTrace.Result.(*TraceResult).Output = common.CopyBytes(output)
			case CREATE:
				topTrace.Result.(*CreateTraceResult).GasUsed = new(hexutil.Big)
				topTrace.Result.(*CreateTraceResult).GasUsed.ToInt().SetUint64(gasUsed)
				topTrace.Result.(*CreateTraceResult).Code = common.CopyBytes(output)
			}
		} else {
			topTrace.Result = nil
			topTrace.Error = err.Error()
		}
	} else {
		if len(output) > 0 {
			switch topTrace.Type {
			case CALL:
				topTrace.Result.(*TraceResult).Output = common.CopyBytes(output)
			case CREATE:
				topTrace.Result.(*CreateTraceResult).Code = common.CopyBytes(output)
			}
		}
		switch topTrace.Type {
		case CALL:
			topTrace.Result.(*TraceResult).GasUsed = new(hexutil.Big)
			topTrace.Result.(*TraceResult).GasUsed.ToInt().SetUint64(gasUsed)
		case CREATE:
			topTrace.Result.(*CreateTraceResult).GasUsed = new(hexutil.Big)
			topTrace.Result.(*CreateTraceResult).GasUsed.ToInt().SetUint64(gasUsed)
		}
	}
	tt.traceStack = tt.traceStack[:len(tt.traceStack)-1]
	if deep {
		tt.traceAddr = tt.traceAddr[:len(tt.traceAddr)-1]
	}
}

func (tt *TxTracer) OnOpcode(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	memory := scope.MemoryData()
	st := scope.StackData()

	if tt.callResult.VmTrace != nil {
		var vmTrace *VmTrace
		if len(tt.vmOpStack) > 0 {
			vmTrace = tt.vmOpStack[len(tt.vmOpStack)-1].Sub
		} else {
			vmTrace = tt.callResult.VmTrace
		}
		if tt.lastVmOp != nil && tt.lastVmOp.Ex != nil {
			// Set the "push" of the last operation
			var showStack int
			switch {
			case tt.lastOp >= vm.PUSH0 && tt.lastOp <= vm.PUSH32:
				showStack = 1
			case tt.lastOp >= vm.SWAP1 && tt.lastOp <= vm.SWAP16:
				showStack = int(tt.lastOp-vm.SWAP1) + 2
			case tt.lastOp >= vm.DUP1 && tt.lastOp <= vm.DUP16:
				showStack = int(tt.lastOp-vm.DUP1) + 2
			}
			switch tt.lastOp {
			case vm.CALLDATALOAD, vm.SLOAD, vm.MLOAD, vm.CALLDATASIZE, vm.LT, vm.GT, vm.DIV, vm.SDIV, vm.SAR, vm.AND, vm.EQ, vm.CALLVALUE, vm.ISZERO,
				vm.ADD, vm.EXP, vm.CALLER, vm.KECCAK256, vm.SUB, vm.ADDRESS, vm.GAS, vm.MUL, vm.RETURNDATASIZE, vm.NOT, vm.SHR, vm.SHL,
				vm.EXTCODESIZE, vm.SLT, vm.OR, vm.NUMBER, vm.PC, vm.TIMESTAMP, vm.BALANCE, vm.SELFBALANCE, vm.MULMOD, vm.ADDMOD, vm.BASEFEE,
				vm.BLOCKHASH, vm.BYTE, vm.XOR, vm.ORIGIN, vm.CODESIZE, vm.MOD, vm.SIGNEXTEND, vm.GASLIMIT, vm.DIFFICULTY, vm.SGT, vm.GASPRICE,
				vm.MSIZE, vm.EXTCODEHASH, vm.SMOD, vm.CHAINID, vm.COINBASE:
				showStack = 1
			}
			for i := showStack - 1; i >= 0; i-- {
				if len(st) > i {
					tt.lastVmOp.Ex.Push = append(tt.lastVmOp.Ex.Push, stackBack(st, i).Hex())
				}
			}
			// Set the "mem" of the last operation
			var setMem bool
			switch tt.lastOp {
			case vm.MSTORE, vm.MSTORE8, vm.MLOAD, vm.RETURNDATACOPY, vm.CALLDATACOPY, vm.CODECOPY, vm.EXTCODECOPY:
				setMem = true
			}
			if setMem && tt.lastMemLen > 0 {
				cpy, err := getMemoryCopyPadded(memory, int64(tt.lastMemOff), int64(tt.lastMemLen))
				if err != nil {
					log.Warn("Failed to copy memory for trace output; this may happen with invalid offset/length",
						"off", tt.lastMemOff,
						"len", tt.lastMemLen,
						"err", err,
						"hint", "May affect trace completeness; consider enabling debug logs for deeper insight")
					cpy = make([]byte, tt.lastMemLen)
				}
				if len(cpy) == 0 {
					cpy = make([]byte, tt.lastMemLen)
				}
				tt.lastVmOp.Ex.Mem = &VmTraceMem{Data: fmt.Sprintf("0x%0x", cpy), Off: int(tt.lastMemOff)}
			}
		}
		if tt.lastOffStack != nil {
			tt.lastOffStack.Ex.Used = int(gas)
			if len(st) > 0 {
				tt.lastOffStack.Ex.Push = []string{stackBack(st, 0).Hex()}
			} else {
				tt.lastOffStack.Ex.Push = []string{}
			}
			if tt.lastMemLen > 0 && memory != nil {
				cpy, _ := getMemoryCopyPadded(memory, int64(tt.lastMemOff), int64(tt.lastMemLen))
				if len(cpy) == 0 {
					cpy = make([]byte, tt.lastMemLen)
				}
				tt.lastOffStack.Ex.Mem = &VmTraceMem{Data: fmt.Sprintf("0x%0x", cpy), Off: int(tt.lastMemOff)}
			}
			tt.lastOffStack = nil
		}
		if tt.lastOp == vm.STOP && vm.OpCode(op) == vm.STOP && len(tt.vmOpStack) == 0 {
			// Looks like OE is "optimising away" the second STOP
			return
		}
		tt.lastVmOp = &VmTraceOp{Ex: &VmTraceEx{}}
		vmTrace.Ops = append(vmTrace.Ops, tt.lastVmOp)

		var sb strings.Builder
		sb.Grow(len(tt.idx))
		for _, idx := range tt.idx {
			sb.WriteString(idx)
		}
		tt.lastVmOp.Idx = fmt.Sprintf("%s%d", sb.String(), len(vmTrace.Ops)-1)

		tt.lastOp = vm.OpCode(op)
		tt.lastVmOp.Cost = int(cost)
		tt.lastVmOp.Pc = int(pc)
		tt.lastVmOp.Ex.Push = []string{}
		tt.lastVmOp.Ex.Used = int(gas) - int(cost)
		tt.lastVmOp.Op = vm.OpCode(op).String()

		switch vm.OpCode(op) {
		case vm.MSTORE, vm.MLOAD:
			if len(st) > 0 {
				tt.lastMemOff = stackBack(st, 0).Uint64()
				tt.lastMemLen = 32
			}
		case vm.MSTORE8:
			if len(st) > 0 {
				tt.lastMemOff = stackBack(st, 0).Uint64()
				tt.lastMemLen = 1
			}
		case vm.RETURNDATACOPY, vm.CALLDATACOPY, vm.CODECOPY:
			if len(st) > 2 {
				tt.lastMemOff = stackBack(st, 0).Uint64()
				tt.lastMemLen = stackBack(st, 2).Uint64()
			}
		case vm.EXTCODECOPY:
			if len(st) > 3 {
				tt.lastMemOff = stackBack(st, 1).Uint64()
				tt.lastMemLen = stackBack(st, 3).Uint64()
			}
		case vm.STATICCALL, vm.DELEGATECALL:
			if len(st) > 5 {
				tt.memOffStack = append(tt.memOffStack, stackBack(st, 4).Uint64())
				tt.memLenStack = append(tt.memLenStack, stackBack(st, 5).Uint64())
			}
		case vm.CALL, vm.CALLCODE:
			if len(st) > 6 {
				tt.memOffStack = append(tt.memOffStack, stackBack(st, 5).Uint64())
				tt.memLenStack = append(tt.memLenStack, stackBack(st, 6).Uint64())
			}
		case vm.CREATE, vm.CREATE2, vm.SELFDESTRUCT:
			// Effectively disable memory output
			tt.memOffStack = append(tt.memOffStack, 0)
			tt.memLenStack = append(tt.memLenStack, 0)
		case vm.SSTORE:
			if len(st) > 1 {
				tt.lastVmOp.Ex.Store = &VmTraceStore{Key: stackBack(st, 0).Hex(), Val: stackBack(st, 1).Hex()}
			}
		}
		if tt.lastVmOp.Ex.Used < 0 {
			tt.lastVmOp.Ex = nil
		}
	}
}

func (tt *TxTracer) GetResult() (json.RawMessage, error) {
	return json.RawMessage{}, nil
}

func (tt *TxTracer) Stop(err error) {}

func (tt *TxTracer) Tracer() *Tracer {
	return &Tracer{
		Hooks: &tracing.Hooks{
			OnEnter:  tt.OnEnter,
			OnExit:   tt.OnExit,
			OnOpcode: tt.OnOpcode,
		},
		GetResult: tt.GetResult,
		Stop:      tt.Stop,
	}
}
