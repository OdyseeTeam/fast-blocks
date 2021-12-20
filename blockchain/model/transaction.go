package model

import "time"

type Transaction struct {
	BlockHash string
	Hash      string
	Version   uint32
	IsSegWit  bool
	InputCnt  uint64
	Inputs    struct {
		TxID string
		Vout int
	}
	OutputCnt uint64
	Outputs   struct {
		TxID string
		Vout int
	}
	Witnesses []Witness
	LockTime  time.Time
}

type Witness struct {
	Bytes []byte
}
