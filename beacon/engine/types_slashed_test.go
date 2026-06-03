package engine

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

func TestSlashedValidatorEntryJSON(t *testing.T) {
	raw := `{"index":"0x0","validatorIndex":"0x2a","address":"0x0000000000000000000000000000000000000000","amount":"0x3b9aca00"}`
	var entry SlashedValidatorEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	w := types.Withdrawal(entry)
	if w.Validator != 42 || w.Amount != 1_000_000_000 {
		t.Fatalf("got validator=%d amount=%d", w.Validator, w.Amount)
	}
	out, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round SlashedValidatorEntry
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("round-trip: %v", err)
	}
	if round != entry {
		t.Fatalf("round-trip mismatch: %+v vs %+v", round, entry)
	}
}

func TestValidateExecutableDataForkFieldsSlashed(t *testing.T) {
	const unknownChainID uint64 = 1
	data := ExecutableData{Timestamp: 0, Slashed: []SlashedValidatorEntry{
		SlashedValidatorEntry(types.Withdrawal{Validator: 1, Amount: 1, Address: common.Address{}}),
	}}
	if err := ValidateExecutableDataForkFields(unknownChainID, data); err == nil {
		t.Fatal("expected error on unknown chain")
	}

	chainID := params.ZGDevnetChainID
	at := params.SlashedForkTimestamp(chainID)
	data.Timestamp = at
	if err := ValidateExecutableDataForkFields(chainID, data); err != nil {
		t.Fatalf("expected ok at fork on devnet: %v", err)
	}
}
