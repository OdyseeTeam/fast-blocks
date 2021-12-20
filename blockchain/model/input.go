package model

import "fast-blocks/blockchain/script"

type Input struct {
	BlockHash       string
	TransactionHash string
	TxRef           string
	Position        uint32
	Script          *script.Hex
	Sequence        uint32
}
