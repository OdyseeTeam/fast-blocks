package stream

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"time"

	"github.com/OdyseeTeam/fast-blocks/blockchain/model"
	"github.com/OdyseeTeam/fast-blocks/lbrycrd"
	"github.com/cockroachdb/errors"
	"github.com/lbryio/lbcd/chaincfg"
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/txscript"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ripemd160"
)

type Blocks interface {
	NextBlock() (*model.Block, error)
	BlockFile() string
}

type blockStream struct {
	lastBlockHash  string
	fileNr         int
	blockNr        int
	startingHeight int
	offset         int64
	path           string
	file           *os.File
	data           *bytes.Buffer
	io.ReadCloser
	io.Seeker
}

func New(path string, fileNr, height int, data []byte) (Blocks, error) {
	if len(data) == 0 {
		file, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		return &blockStream{path: path, file: file, fileNr: fileNr, blockNr: height}, nil
	}

	return &blockStream{path: path, data: bytes.NewBuffer(data)}, nil
}

func (bs blockStream) BlockFile() string {
	return bs.file.Name()
}

func (bs *blockStream) NextBlock() (*model.Block, error) {
	block := &model.Block{Height: bs.blockNr}

	// this is wrong. the blocks are not stored in order in the file
	// use the leveldb index if you want to read the blocks in order
	// see chain.blockFilesOrderedByHeight() for a starting point
	bs.blockNr = bs.blockNr + 1

	err := bs.setBlockInfo(block)
	if err != nil {
		return nil, err
	}

	transactions, err := bs.setTransactions(block)
	if err != nil {
		return nil, err
	}
	for _, t := range transactions {
		block.Transactions = append(block.Transactions, t)
	}

	return block, nil //errors.WithStack(storage.DB.Exec(`INSERT INTO blocks VALUES ?`, &block))
}

var magicNumberConst = []byte{250, 228, 170, 241}

func (bs *blockStream) setBlockInfo(block *model.Block) error {
	magicNumber, err := bs.readMagicNumber()
	if err != nil {
		return err
	}
	blockSize, _, err := bs.readUint32()
	if err != nil {
		return err
	}

	header, err := bs.readBytes(112)
	if err != nil {
		return err
	}

	block.Header = header
	block.MagicNumber = magicNumber
	block.BlockSize = blockSize

	prevBlockHash, err := chainhash.NewHash(ReverseBytes(header[4:36]))
	if err != nil {
		return err
	}

	blockHash := chainhash.DoubleHashH(header)
	block.BlockHash = &blockHash
	block.Version = binary.LittleEndian.Uint32(header[0:4])
	block.PrevBlockHash = prevBlockHash
	block.MerkleRoot = header[36:68]
	block.ClaimTrieRoot = header[68:100]
	block.TimeStamp = time.Unix(int64(binary.LittleEndian.Uint32(header[100:104])), 0)
	block.Bits = binary.LittleEndian.Uint32(header[104:108])
	block.Nonce = binary.LittleEndian.Uint32(header[108:112])

	txCnt, _, err := bs.readCompactSize()
	if err != nil {
		return err
	}
	block.TxCnt = int(txCnt)

	// VALIDATION

	for i, _ := range magicNumber {
		if magicNumberConst[i] != magicNumber[i] {
			println("failed to get constant magic number")
		}
	}

	if block.Version > 1 && block.Version != 536870912 && block.Version != 536870913 {
		panic("Block file reading is toast! Version should always be 1 or 536870912,536870913")
	}

	return nil
}

func (bs *blockStream) tell() (int64, error) {
	if bs.data != nil {
		return bs.offset, nil
	}

	return bs.Seek(0, io.SeekCurrent)
}

func (bs *blockStream) Seek(offset int64, whence int) (int64, error) {
	if bs.data != nil {
		return 0, nil
	}

	ret, err := bs.file.Seek(offset, whence)
	return ret, errors.WithStack(err)
}

func (bs *blockStream) Read(p []byte) (n int, err error) {
	if bs.data != nil {
		return bs.data.Read(p)
	}
	read, err := bs.file.Read(p)
	bs.offset += int64(read)
	return read, errors.WithStack(err)
}

func (bs *blockStream) readCompactSize() (uint64, []byte, error) {
	var readBuf []byte
	bSize := make([]byte, 1)
	_, err := bs.Read(bSize)
	if err != nil {
		return 0, nil, err
	}
	readBuf = append(readBuf, bSize...)

	size := uint64(bSize[0])
	if size < 253 {
		return size, readBuf, nil
	}

	if size == 253 {
		buf := make([]byte, 2)
		_, err := bs.Read(buf)
		if err != nil {
			return 0, nil, err
		}
		readBuf = append(readBuf, buf...)

		return uint64(binary.LittleEndian.Uint16(buf)), readBuf, nil
	}
	if size == 254 {
		v, buf, err := bs.readUint32()
		if err != nil {
			return 0, nil, err
		}
		readBuf = append(readBuf, buf...)
		return uint64(v), readBuf, err
	}

	if size == 255 {
		buf := make([]byte, 8)
		readBuf = append(readBuf, buf...)
		return binary.LittleEndian.Uint64(buf), readBuf, nil
	}

	return 0, nil, errors.New("size is greater than 255")
}

func (bs *blockStream) setTransactions(block *model.Block) ([]model.Transaction, error) {
	var err error
	var transactions []model.Transaction

	for i := 0; i < block.TxCnt; i++ {
		var outputs []model.Output
		var inputs []model.Input
		tx := model.Transaction{}
		var txBytes []byte
		var buf []byte

		tx.Version, buf, err = bs.readUint32()
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)

		tx.InputCnt, buf, err = bs.readCompactSize()
		if err != nil {
			return nil, err
		}

		if tx.InputCnt == 0 {
			tx.IsSegWit, buf, err = bs.readBool()
			if err != nil {
				return nil, err
			}
			if !tx.IsSegWit {
				panic("zero inputs and not segwit!!")
			}
			//txBytes = append(txBytes, buf...) Not included for Segwit TxHash

			tx.InputCnt, buf, err = bs.readCompactSize()
			if err != nil {
				return nil, err
			}
			txBytes = append(txBytes, buf...) // From Segwit input count

			txBytes, inputs, err = bs.setInputs(&tx, txBytes)
			if err != nil {
				return nil, err
			}

		} else {
			txBytes = append(txBytes, buf...) // From normal input count
			txBytes, inputs, err = bs.setInputs(&tx, txBytes)
			if err != nil {
				return nil, err
			}
		}

		tx.OutputCnt, buf, err = bs.readCompactSize()
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)

		txBytes, outputs, err = bs.setOutputs(&tx, txBytes)
		if err != nil {
			return nil, err
		}

		if tx.IsSegWit {
			for i := 0; i < int(tx.InputCnt); i++ {
				nrWitnesses, _, err := bs.readCompactSize()
				if err != nil {
					return nil, err
				}

				for i := 0; i < int(nrWitnesses); i++ {
					witness := model.Witness{}
					size, _, err := bs.readCompactSize()
					if err != nil {
						return nil, err
					}

					witness.Bytes, err = bs.readBytes(int(size))
					if err != nil {
						return nil, err
					}

					tx.Witnesses = append(tx.Witnesses, witness)
				}
			}
		}

		lockTimeBytes, buf, err := bs.readUint32()
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)

		txHash := chainhash.DoubleHashH(txBytes)
		tx.Hash = &txHash
		tx.BlockHash = block.BlockHash
		for _, out := range outputs {
			out.TransactionHash = tx.Hash
			out.BlockHash = block.BlockHash
			tx.Outputs = append(tx.Outputs, out)
			//err := storage.DB.Exec(`INSERT INTO outputs VALUES ?`, &o)
			//if err != nil {
			//	return nil, errors.Err(err)
			//}
		}
		for _, in := range inputs {
			in.TransactionHash = tx.Hash
			in.BlockHash = block.BlockHash
			tx.Inputs = append(tx.Inputs, in)
			//err := storage.DB.Exec(`INSERT INTO inputs VALUES ?`, &i)
			//if err != nil {
			//	return nil, errors.Err(err)
			//}
		}

		tx.LockTime = time.Unix(int64(lockTimeBytes), 0)

		//err = storage.DB.Exec(`INSERT INTO transactions VALUES ?`, &tx)
		//if err != nil {
		//	return nil, errors.WithStack(err)
		//}

		transactions = append(transactions, tx)
	}
	return transactions, nil
}

func (bs *blockStream) setInputs(tx *model.Transaction, txBytes []byte) ([]byte, []model.Input, error) {
	var err error
	var inputs []model.Input
	for i := 0; i < int(tx.InputCnt); i++ {
		var buf []byte
		in := model.Input{}

		buf, err = bs.readBytes(32) //TxID
		if err != nil {
			return nil, nil, err
		}
		txBytes = append(txBytes, buf...)
		in.TxRef, _ = chainhash.NewHash(buf)

		in.Position, buf, err = bs.readUint32()
		if err != nil {
			return nil, nil, err
		}
		txBytes = append(txBytes, buf...)

		scriptLength, buf, err := bs.readCompactSize()
		if err != nil {
			return nil, nil, err
		}
		txBytes = append(txBytes, buf...)

		scriptBytes, err := bs.readBytes(int(scriptLength))
		if err != nil {
			return nil, nil, err
		}
		txBytes = append(txBytes, scriptBytes...)
		in.Script = scriptBytes

		in.Sequence, buf, err = bs.readUint32R()
		if err != nil {
			return nil, nil, err
		}
		txBytes = append(txBytes, buf...)

		inputs = append(inputs, in)
	}
	return txBytes, inputs, nil
}

func (bs *blockStream) setOutputs(tx *model.Transaction, txBytes []byte) ([]byte, []model.Output, error) {
	var err error
	var outputs []model.Output
	for i := 0; i < int(tx.OutputCnt); i++ {
		var buf []byte
		out := model.Output{}
		out.Amount, buf, err = bs.readUint64()
		if err != nil {
			return nil, nil, err
		}
		txBytes = append(txBytes, buf...)

		scriptLength, buf, err := bs.readCompactSize()
		if err != nil {
			return nil, nil, err
		}
		txBytes = append(txBytes, buf...)

		scriptBytes, err := bs.readBytes(int(scriptLength))
		if err != nil {
			return nil, nil, err
		}
		txBytes = append(txBytes, scriptBytes...)
		out.PKScript = scriptBytes
		//logrus.Traceln(txscript.DisasmString(scriptBytes))

		scriptType, addresses, _, err := txscript.ExtractPkScriptAddrs(scriptBytes, &chaincfg.MainNetParams)
		if err != nil {
			return nil, nil, err
		}
		out.ScriptType = scriptType.String()

		if scriptType != txscript.NonStandardTy {
			if len(addresses) == 1 {
				out.Address = addresses[0]
			}

			claimScript, err := txscript.ExtractClaimScript(scriptBytes)
			if err != nil {
				claimScript = nil
			}

			if claimScript != nil {
				out.ClaimScript = claimScript
			} else if lbrycrd.IsPurchaseScript(scriptBytes) {
				purchase, err := lbrycrd.ParsePurchaseScript(scriptBytes)
				if err != nil {
					return nil, nil, err
				}
				logrus.Debugln("Purchase: ", purchase.ClaimHash)
			}
		}

		outputs = append(outputs, out)
	}
	return txBytes, outputs, nil
}

func (bs *blockStream) readBytes(toRead int) ([]byte, error) {
	buf := make([]byte, toRead)
	_, err := bs.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (bs *blockStream) readMagicNumber() ([]byte, error) {
	var pos = 0
	for pos < 4 {
		magicPosByte := magicNumberConst[pos]
		buf, err := bs.readBytes(1)
		if err != nil {
			return nil, err
		}
		if magicPosByte == buf[0] {
			pos++
		} else if pos == 4 {
			break
		} else if pos > 0 {
			if buf[0] == magicNumberConst[0] {
				pos = 1
			} else {
				pos = 0 /// A, B, C, D => A, B, A, B, C, D
			}
		}
	}
	return magicNumberConst, nil
}

func (bs *blockStream) readUint64() (uint64, []byte, error) {
	buf, err := bs.readBytes(8)
	if err != nil {
		return 0, nil, err
	}
	return binary.LittleEndian.Uint64(buf), buf, nil
}

func (bs *blockStream) readUint32() (uint32, []byte, error) {
	buf, err := bs.readBytes(4)
	if err != nil {
		return 0, nil, err
	}
	return binary.LittleEndian.Uint32(buf), buf, nil
}

func (bs *blockStream) readUint32R() (uint32, []byte, error) {
	buf, err := bs.readBytes(4)
	if err != nil {
		return 0, nil, err
	}
	return binary.LittleEndian.Uint32(ReverseBytes(buf)), buf, nil
}

func (bs *blockStream) readUint8() (uint8, []byte, error) {
	buf, err := bs.readBytes(1)
	if err != nil {
		return 0, nil, err
	}
	return buf[0], buf, nil
}

func (bs *blockStream) readBool() (bool, []byte, error) {
	v, buf, err := bs.readUint8()
	if err != nil {
		return false, nil, err
	}
	if v > 1 {
		return false, nil, errors.WithStack(errors.New("meant to parse boolean but found byte greater than 1"))
	}
	if v == 0 {
		return false, buf, nil
	}
	return true, buf, nil
}

func (bs *blockStream) readString() (string, []byte, error) {
	size, readBuf, err := bs.readCompactSize()
	if err != nil {
		return "", nil, err
	}

	buf, err := bs.readBytes(int(size))
	if err != nil {
		return "", nil, err
	}
	readBuf = append(readBuf, buf...)

	return string(buf), readBuf, nil
}

func IsCoinbaseInput(txid *chainhash.Hash) bool {
	for _, b := range txid {
		if b != 0 {
			return false
		}
	}
	return true
}

// ReverseBytes reverses a byte slice. useful for switching endian-ness
func ReverseBytes(b []byte) []byte {
	r := make([]byte, len(b))
	for left, right := 0, len(b)-1; left < right; left, right = left+1, right-1 {
		r[left], r[right] = b[right], b[left]
	}
	return r
}

func ClaimIDFromOutpoint(txid string, nout int) (string, error) {
	// convert transaction id to byte array
	txidBytes, err := hex.DecodeString(txid)
	if err != nil {
		return "", err
	}

	// reverse (make big-endian)
	txidBytes = ReverseBytes(txidBytes)

	// append nout
	noutBytes := make([]byte, 4) // num bytes in uint32
	binary.BigEndian.PutUint32(noutBytes, uint32(nout))
	txidBytes = append(txidBytes, noutBytes...)

	// sha256 it
	s := sha256.New()
	s.Write(txidBytes)

	// ripemd it
	r := ripemd160.New()
	r.Write(s.Sum(nil))

	// reverse (make little-endian)
	res := ReverseBytes(r.Sum(nil))

	return hex.EncodeToString(res), nil
}
