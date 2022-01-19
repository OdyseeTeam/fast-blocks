package model

import (
	"time"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
)

type Block struct {
	Header        []byte
	MagicNumber   []byte
	BlockSize     uint32
	Version       uint32
	Bits          uint32
	Nonce         uint32
	TimeStamp     time.Time
	Height        int
	FileNumber    int
	BlockHash     chainhash.Hash
	PrevBlockHash chainhash.Hash
	MerkleRoot    []byte
	ClaimTrieRoot []byte
	Transactions  []Transaction
	TxCnt         int
}

func (b Block) String() string {
	return ""
}
