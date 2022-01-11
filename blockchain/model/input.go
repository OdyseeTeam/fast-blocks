package model

import "github.com/OdyseeTeam/fast-blocks/blockchain/script"

type Input struct {
	BlockHash       string
	TransactionHash string
	TxRef           string
	Position        uint32
	Script          *script.Hex
	Sequence        uint32
}
