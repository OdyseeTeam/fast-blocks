package model

import (
	"encoding/hex"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
)

type Input struct {
	BlockHash       chainhash.Hash
	TransactionHash chainhash.Hash
	TxRef           string
	Position        uint32
	Script          Script
	Sequence        uint32
}

type Script []byte

func (s Script) String() string { return hex.EncodeToString(s) }
func (s Script) Bytes() []byte  { return s }
