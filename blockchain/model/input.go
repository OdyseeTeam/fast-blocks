package model

import "fast-blocks/blockchain/script"

type Input struct {
	TxRef    string
	Position uint32
	Script   *script.Hex
	Sequence uint32
}
