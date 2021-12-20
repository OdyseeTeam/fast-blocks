package model

import pb "github.com/lbryio/types/v2/go"

type Output struct {
	BlockHash       string
	TransactionHash string
	Amount          uint64
	Address         Address
	ScriptType      string
	PKScript        []byte
	Claim           *pb.Claim
	Purchase        *pb.Purchase
}
