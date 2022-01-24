package lbrycrd

import (
	"encoding/hex"
	"testing"

	"github.com/OdyseeTeam/fast-blocks/blockchain"
)

func TestPurchaseScriptParse(t *testing.T) {
	hexStr := "6a17500a14b5fb292f0ccb678a0c393b5ab47c522d1a9f4bfc"
	hexBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		t.Fatal(err)
	}
	isPurchase := IsPurchaseScript(hexBytes)
	if !isPurchase {
		t.Fatal("test string no longer identifies as a purchase!")
	}
	purchase, err := ParsePurchaseScript(hexBytes)
	if err != nil {
		t.Fatal(err)
	}
	purchase.GetClaimHash()
	bytes := blockchain.ReverseBytes(purchase.GetClaimHash())
	claimID := hex.EncodeToString(bytes)
	expectedClaimID := "fc4b9f1a2d527cb45a3b390c8a67cb0c2f29fbb5"
	if claimID != expectedClaimID {
		t.Errorf("expected %s, got %s", expectedClaimID, claimID)
	}
}
