package model

import "time"

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
	BlockHash     string
	PrevBlockHash string
	MerkleRoot    string
	ClaimTrieRoot string
	Transactions  []Transaction
	TxCnt         int
}

func (b Block) String() string {
	return ""
}
