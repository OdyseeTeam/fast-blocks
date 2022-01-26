package blockchain

import (
	"encoding/hex"
	"time"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/txscript"
	"github.com/lbryio/lbcutil"
	pb "github.com/lbryio/types/v2/go"
)

type Transaction struct {
	Hash        *chainhash.Hash
	Version     uint32
	IsSegWit    bool
	InputCount  uint64
	Inputs      []Input
	OutputCount uint64
	Outputs     []Output
	Witnesses   []Witness
	LockTime    time.Time
}

type Witness []byte

type Header struct {
	Version       uint32
	Bits          uint32
	Nonce         uint32
	TimeStamp     time.Time
	BlockHash     *chainhash.Hash
	PrevBlockHash *chainhash.Hash
	MerkleRoot    []byte
	ClaimTrieRoot []byte
}

type Block struct {
	Height       uint64
	Size         uint32
	Header       *Header
	Transactions []Transaction
}

func (b Block) String() string { return "" }

type Input struct {
	PrevTxHash  *chainhash.Hash
	PrevTxIndex uint32
	Script      Script
	Sequence    uint32
}

func (i Input) IsCoinbase() bool {
	for _, b := range i.PrevTxHash {
		if b != 0 {
			return false
		}
	}
	return true
}

type Script []byte

func (s Script) String() string { return hex.EncodeToString(s) }
func (s Script) Bytes() []byte  { return s }

type Output struct {
	Amount      uint64
	Address     lbcutil.Address
	ScriptClass txscript.ScriptClass
	PKScript    Script
	ClaimScript *txscript.ClaimScript
	Purchase    *pb.Purchase
}
