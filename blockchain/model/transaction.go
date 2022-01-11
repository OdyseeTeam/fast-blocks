package model

import "time"

type Transaction struct {
	BlockHash string
	Hash      string
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
