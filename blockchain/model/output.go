package model

import (
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	pb "github.com/lbryio/types/v2/go"
)

type Output struct {
	BlockHash       chainhash.Hash
	TransactionHash chainhash.Hash
	Amount          uint64
	Address         Address
	ScriptType      string
	PKScript        Script
	Claim           *pb.Claim
	Purchase        *pb.Purchase
}
