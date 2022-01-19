package model

import (
	"encoding/hex"
	"time"

	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/txscript"
	"github.com/lbryio/lbcutil"
	pb "github.com/lbryio/types/v2/go"
)

type Transaction struct {
	BlockHash *chainhash.Hash
	Hash      *chainhash.Hash
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
	BlockHash     *chainhash.Hash
	PrevBlockHash *chainhash.Hash
	MerkleRoot    []byte
	ClaimTrieRoot []byte
	Transactions  []Transaction
	TxCnt         int
}

func (b Block) String() string { return "" }

type Input struct {
	BlockHash       *chainhash.Hash
	TransactionHash *chainhash.Hash
	TxRef           *chainhash.Hash
	Position        uint32
	Script          Script
	Sequence        uint32
}

type Script []byte

func (s Script) String() string { return hex.EncodeToString(s) }
func (s Script) Bytes() []byte  { return s }

type Output struct {
	BlockHash       *chainhash.Hash
	TransactionHash *chainhash.Hash
	Amount          uint64
	Address         lbcutil.Address
	ScriptType      string
	PKScript        Script
	ClaimScript     *txscript.ClaimScript
	Purchase        *pb.Purchase
}
