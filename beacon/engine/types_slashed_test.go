package engine

import (
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

func TestSlashedValidatorEntryJSON(t *testing.T) {
	raw := `{"validator_index":"42","penalty_gwei":"1000000000"}`
	var entry SlashedValidatorEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.ValidatorIndex != 42 || entry.PenaltyGwei != 1_000_000_000 {
		t.Fatalf("got index=%d penalty=%d", entry.ValidatorIndex, entry.PenaltyGwei)
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
	chainID := params.ZGMainnetChainID
	before := params.SlashedForkTimestamp(chainID) - 1
	at := params.SlashedForkTimestamp(chainID)

	data := ExecutableData{Timestamp: before, Slashed: []SlashedValidatorEntry{{ValidatorIndex: 1, PenaltyGwei: 1}}}
	if err := ValidateExecutableDataForkFields(chainID, data); err == nil {
		t.Fatal("expected error before fork")
	}
	data.Timestamp = at
	if err := ValidateExecutableDataForkFields(chainID, data); err != nil {
		t.Fatalf("expected ok at fork: %v", err)
	}
}
