package model

import (
	"time"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
)

type Transaction struct {
	BlockHash chainhash.Hash
	Hash      chainhash.Hash
	Version   uint32
	IsSegWit  bool
	InputCnt  uint64
	Inputs    []Input
	OutputCnt uint64
	Outputs   []Output
	Witnesses []Witness
	LockTime  time.Time
}

type Witness struct {
	Bytes []byte
}
