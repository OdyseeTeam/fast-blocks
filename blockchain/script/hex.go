package script

import "encoding/hex"

type Hex struct {
	bytes []byte
	hex   string
}

func ToHex(b []byte) *Hex {
	return &Hex{
		bytes: b,
		hex:   hex.EncodeToString(b),
	}
}

func (h Hex) String() string {
	return h.hex
}

func (h Hex) Bytes() []byte {
	return h.bytes
}
