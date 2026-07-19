package transactions

import (
	"math/big"
	"testing"
)

func TestSettlementOperationIDIsDeterministicAndDomainSeparated(t *testing.T) {
	vault := "0x1000000000000000000000000000000000000001"
	intent := "settlement_intent_1"
	token := "0x2000000000000000000000000000000000000002"
	recipient := "0x3000000000000000000000000000000000000003"
	amount := big.NewInt(1_000_000)

	first, err := SettlementOperationID(56, vault, intent, token, recipient, amount)
	if err != nil {
		t.Fatalf("SettlementOperationID returned error: %v", err)
	}
	second, err := SettlementOperationID(56, vault, intent, token, recipient, amount)
	if err != nil {
		t.Fatalf("SettlementOperationID returned error on second call: %v", err)
	}
	if first != second {
		t.Fatalf("expected deterministic operation id")
	}

	polygon, err := SettlementOperationID(137, vault, intent, token, recipient, amount)
	if err != nil {
		t.Fatalf("SettlementOperationID returned polygon error: %v", err)
	}
	if polygon == first {
		t.Fatalf("expected chainID to domain-separate operation id")
	}

	otherAmount, err := SettlementOperationID(56, vault, intent, token, recipient, big.NewInt(2_000_000))
	if err != nil {
		t.Fatalf("SettlementOperationID returned amount error: %v", err)
	}
	if otherAmount == first {
		t.Fatalf("expected amount to domain-separate operation id")
	}
}

func TestSettlementOperationIDRejectsInvalidInput(t *testing.T) {
	if _, err := SettlementOperationID(0, "", "", "", "", nil); err == nil {
		t.Fatalf("expected invalid input error")
	}
}
