package stream

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fast-blocks/blockchain/model"
	"fast-blocks/blockchain/script"
	"fast-blocks/util"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/lbryio/lbry.go/v2/extras/errors"
	"io"
	"os"
	"time"
)

type Blocks interface {
	NextBlock() (*model.Block, error)
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
			return nil, errors.Err(err)
		}
		return &blockStream{path: path, file: file, fileNr: fileNr, blockNr: height}, nil
	}

	return &blockStream{path: path, data: bytes.NewBuffer(data)}, nil
}

func (bs *blockStream) NextBlock() (*model.Block, error) {
	block := &model.Block{Height: bs.blockNr}
	bs.blockNr = bs.blockNr + 1
	err := bs.setBlockInfo(block)
	if err != nil {
		return nil, err
	}

	err = bs.setTransactions(block)
	if err != nil {
		return nil, err
	}

	return block, nil
}

var magicNumberConst = []byte{250, 228, 170, 241}

func (bs *blockStream) setBlockInfo(block *model.Block) error {
	if block.Height == 3992 {
		println("problematic block")
	}
	magicNumber, err := bs.readMagicNumber()
	if err != nil {
		return errors.Err(err)
	}
	blockSize, _, err := bs.readUint32()
	if err != nil {
		return errors.Err(err)
	}

	header, err := bs.readBytes(112)
	if err != nil {
		return errors.Err(err)
	}

	block.Header = header
	block.MagicNumber = magicNumber
	block.BlockSize = blockSize

	block.BlockHash = chainhash.DoubleHashH(header).String()
	block.Version = binary.LittleEndian.Uint32(header[0:4])
	block.PrevBlockHash = hex.EncodeToString(util.ReverseBytes(header[4:36]))
	block.MerkleRoot = hex.EncodeToString(header[36:68])
	block.ClaimTrieRoot = hex.EncodeToString(header[68:100])
	block.TimeStamp = time.Unix(int64(binary.LittleEndian.Uint32(header[100:104])), 0)
	block.Bits = binary.LittleEndian.Uint32(header[104:108])
	block.Nonce = binary.LittleEndian.Uint32(header[108:112])

	txCnt, _, err := bs.readCompactSize()
	if err != nil {
		return errors.Err(err)
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

	offset, err := bs.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, errors.Err(err)
	}
	return offset, nil
}

func (bs *blockStream) Seek(offset int64, whence int) (int64, error) {
	if bs.data != nil {
		return 0, nil
	}
	return bs.file.Seek(offset, whence)
}

func (bs *blockStream) Read(p []byte) (n int, err error) {
	if bs.data != nil {
		return bs.data.Read(p)
	}
	read, err := bs.file.Read(p)
	bs.offset += int64(read)
	return read, errors.Err(err)
}

func (bs *blockStream) readCompactSize() (uint64, []byte, error) {
	var readBuf []byte
	bSize := make([]byte, 1)
	_, err := bs.Read(bSize)
	if err != nil {
		return 0, nil, errors.Err(err)
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
			return 0, nil, errors.Err(err)
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
		//readBuf = append(readBuf, buf...)
		return binary.LittleEndian.Uint64(buf), readBuf, nil
	}

	return 0, nil, errors.Err("size is greater than 255")
}

func (bs *blockStream) setTransactions(block *model.Block) error {
	var err error

	for i := 0; i < block.TxCnt; i++ {
		tx := &model.Transaction{}
		var txBytes []byte
		var buf []byte

		tx.Version, buf, err = bs.readUint32()
		if err != nil {
			return err
		}
		txBytes = append(txBytes, buf...)

		tx.InputCnt, buf, err = bs.readCompactSize()
		if err != nil {
			return err
		}
		txBytes = append(txBytes, buf...)

		if tx.InputCnt == 0 {
			tx.IsSegWit, buf, err = bs.readBool()
			if err != nil {
				return err
			}
			txBytes = append(txBytes, buf...)

			tx.InputCnt, buf, err = bs.readCompactSize()
			if err != nil {
				return err
			}
			txBytes = append(txBytes, buf...)
		}

		txBytes, err = bs.setInputs(tx, txBytes)
		if err != nil {
			return err
		}

		tx.OutputCnt, buf, err = bs.readCompactSize()
		if err != nil {
			return err
		}
		txBytes = append(txBytes, buf...)

		if i == 19 && block.BlockHash == "b36031249a6675103c4e0c0225abfe65b7d1c43b15be3ca7f19478afc7703146" {
			println("catch me here")
		}

		txBytes, err = bs.setOutputs(tx, txBytes)
		if err != nil {
			return err
		}

		if tx.IsSegWit {
			panic("need to handle segwit buf returns properly")
			for range tx.Inputs {
				nrWitnesses, _, err := bs.readCompactSize()
				if err != nil {
					return err
				}
				for i := 0; i < int(nrWitnesses); i++ {
					witness := model.Witness{}
					size, _, err := bs.readCompactSize()
					if err != nil {
						return err
					}
					witness.Bytes, err = bs.readBytes(int(size))
					if err != nil {
						return err
					}
					tx.Witnesses = append(tx.Witnesses, witness)
				}
			}
		}

		lockTimeBytes, buf, err := bs.readUint32()
		if err != nil {
			return err
		}
		txBytes = append(txBytes, buf...)

		tx.Hash = chainhash.DoubleHashH(txBytes).String()
		tx.LockTime = time.Unix(int64(lockTimeBytes), 0)

		block.Transactions = append(block.Transactions, tx)
	}
	return nil
}

func (bs *blockStream) setInputs(tx *model.Transaction, txBytes []byte) ([]byte, error) {
	var err error

	for i := 0; i < int(tx.InputCnt); i++ {
		var buf []byte
		in := model.Input{}

		buf, err = bs.readBytes(32) //TxID
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)
		in.TxRef = "Coinbase"
		if !isCoinBase(buf) {
			in.TxRef = hex.EncodeToString(util.ReverseBytes(buf))
		}

		in.Position, buf, err = bs.readUint32R()
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)

		scriptLength, buf, err := bs.readCompactSize()
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)

		scriptBytes, err := bs.readBytes(int(scriptLength))
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, scriptBytes...)
		in.Script = script.ToHex(scriptBytes)

		in.Sequence, buf, err = bs.readUint32R()
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)

		tx.Inputs = append(tx.Inputs, in)
	}
	return txBytes, nil
}

func (bs *blockStream) setOutputs(tx *model.Transaction, txBytes []byte) ([]byte, error) {
	var err error
	for i := 0; i < int(tx.OutputCnt); i++ {
		var buf []byte
		out := model.Output{}
		out.Amount, buf, err = bs.readUint64()
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)

		scriptLength, buf, err := bs.readCompactSize()
		if err != nil {
			return nil, err
		}
		txBytes = append(txBytes, buf...)

		scriptBytes, err := bs.readBytes(int(scriptLength))
		if err != nil {
			return nil, err
		}
		out.Script = script.ToHex(scriptBytes)
		txBytes = append(txBytes, scriptBytes...)

		tx.Outputs = append(tx.Outputs, out)
	}
	return txBytes, nil
}

func (bs *blockStream) readBytes(toRead int) ([]byte, error) {
	buf := make([]byte, toRead)
	_, err := bs.Read(buf)
	if err != nil {
		return nil, errors.Err(err)
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
		return 0, nil, errors.Err(err)
	}
	return binary.LittleEndian.Uint64(buf), buf, nil
}

func (bs *blockStream) readUint32() (uint32, []byte, error) {
	buf, err := bs.readBytes(4)
	if err != nil {
		return 0, nil, errors.Err(err)
	}
	return binary.LittleEndian.Uint32(buf), buf, nil
}

func (bs *blockStream) readUint32R() (uint32, []byte, error) {
	buf, err := bs.readBytes(4)
	if err != nil {
		return 0, nil, errors.Err(err)
	}
	return binary.LittleEndian.Uint32(util.ReverseBytes(buf)), buf, nil
}

func (bs *blockStream) readUint8() (uint8, []byte, error) {
	buf, err := bs.readBytes(1)
	if err != nil {
		return 0, nil, errors.Err(err)
	}
	return buf[0], buf, nil
}

func (bs *blockStream) readBool() (bool, []byte, error) {
	v, buf, err := bs.readUint8()
	if err != nil {
		return false, nil, err
	}
	if v > 1 {
		return false, nil, errors.Err("meant to parse boolean but found byte greater than 1")
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

func isCoinBase(txid []byte) bool {
	for _, b := range txid {
		if b != 0 {
			return false
		}
	}
	return true
}
