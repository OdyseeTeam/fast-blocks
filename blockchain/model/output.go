package model

import "fast-blocks/blockchain/script"

type Output struct {
	Amount uint64
	Script *script.Hex
}
