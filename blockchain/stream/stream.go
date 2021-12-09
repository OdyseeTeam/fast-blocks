package stream

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fast-blocks/blockchain/model"
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

func (bs *blockStream) setBlockInfo(block *model.Block) error {
	magicNumber, err := bs.readBytes(4)
	if err != nil {
		return errors.Err(err)
	}
	blockSize, err := bs.readUint32()
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
	if block.Version > 1 {
		//panic("Block file reading is toast! Version should always be less than 1")
	}
	block.PrevBlockHash = hex.EncodeToString(util.ReverseBytes(header[4:36]))
	block.MerkleRoot = hex.EncodeToString(header[36:68])
	block.ClaimTrieRoot = hex.EncodeToString(header[68:100])
	block.TimeStamp = time.Unix(int64(binary.LittleEndian.Uint32(header[100:104])), 0)
	block.Bits = binary.LittleEndian.Uint32(header[104:108])
	block.Nonce = binary.LittleEndian.Uint32(header[108:112])

	txCnt, err := bs.readCompactSize()
	if err != nil {
		return errors.Err(err)
	}
	block.TxCnt = int(txCnt)

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

func (bs *blockStream) readCompactSize() (uint64, error) {
	bSize := make([]byte, 1)
	_, err := bs.Read(bSize)
	if err != nil {
		return 0, errors.Err(err)
	}
	size := uint64(bSize[0])
	if size < 253 {
		return size, nil
	}

	if size == 253 {
		buf := make([]byte, 2)
		bs.Read(buf)
		return uint64(binary.LittleEndian.Uint16(buf)), nil
	}
	if size == 254 {
		v, err := bs.readUint32()
		return uint64(v), err
	}

	if size == 255 {
		buf := make([]byte, 8)
		return binary.LittleEndian.Uint64(buf), nil
	}

	return 0, errors.Err("size is greater than 255")
}

func (bs *blockStream) setTransactions(block *model.Block) error {
	tx := model.Transaction{}
	var err error
	for i := 0; i < block.TxCnt; i++ {
		tx.Version, err = bs.readUint32()
		if err != nil {
			return err
		}

		tx.InputCnt, err = bs.readCompactSize()
		if err != nil {
			return err
		}
		if tx.InputCnt == 0 {
			tx.IsSegWit, err = bs.readBool()
			if err != nil {
				return err
			}
			tx.InputCnt, err = bs.readCompactSize()
			if err != nil {
				return err
			}
		}
		err = bs.setInputs(&tx)
		if err != nil {
			return err
		}

		tx.OutputCnt, err = bs.readCompactSize()
		if err != nil {
			return err
		}
		err = bs.setOutputs(&tx)
		if err != nil {
			return err
		}

		if tx.IsSegWit {
			for range tx.Inputs {
				nrWitnesses, err := bs.readCompactSize()
				if err != nil {
					return err
				}
				for i := 0; i < int(nrWitnesses); i++ {
					witness := model.Witness{}
					size, err := bs.readCompactSize()
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
		lockTimeBytes, err := bs.readUint32()
		if err != nil {
			return err
		}
		tx.LockTime = time.Unix(int64(lockTimeBytes), 0)

		block.Transactions = append(block.Transactions, tx)
	}
	return nil
}

func (bs *blockStream) setInputs(tx *model.Transaction) error {

	var err error
	for i := 0; i < int(tx.InputCnt); i++ {
		in := model.Input{}
		in.TxRef, err = bs.readBytes(32)
		if err != nil {
			return err
		}
		in.Position, err = bs.readUint32()
		in.Script, err = bs.readString()
		in.Sequence, err = bs.readUint32()

		tx.Inputs = append(tx.Inputs, in)
	}
	return nil
}

func (bs *blockStream) setOutputs(tx *model.Transaction) error {
	var err error
	for i := 0; i < int(tx.OutputCnt); i++ {
		out := model.Output{}
		out.Amount, err = bs.readUint64()
		if err != nil {
			return err
		}
		scriptLength, err := bs.readCompactSize()
		if err != nil {
			return err
		}
		out.Script, err = bs.readBytes(int(scriptLength))
		if err != nil {
			return err
		}
		tx.Outputs = append(tx.Outputs, out)
	}
	return nil
}

func (bs *blockStream) readBytes(toRead int) ([]byte, error) {
	buf := make([]byte, toRead)
	_, err := bs.Read(buf)
	if err != nil {
		return nil, errors.Err(err)
	}
	return buf, nil
}

func (bs *blockStream) readUint64() (uint64, error) {
	buf, err := bs.readBytes(8)
	if err != nil {
		return 0, errors.Err(err)
	}
	return binary.LittleEndian.Uint64(buf), nil
}

func (bs *blockStream) readUint32() (uint32, error) {
	buf, err := bs.readBytes(4)
	if err != nil {
		return 0, errors.Err(err)
	}
	return binary.LittleEndian.Uint32(buf), nil
}

func (bs *blockStream) readUint8() (uint8, error) {
	buf, err := bs.readBytes(1)
	if err != nil {
		return 0, errors.Err(err)
	}
	return buf[0], nil
}

func (bs *blockStream) readBool() (bool, error) {
	v, err := bs.readUint8()
	if err != nil {
		return false, err
	}
	if v > 1 {
		return false, errors.Err("meant to parse boolean but found byte greater than 1")
	}
	if v == 0 {
		return false, nil
	}
	return true, nil
}

func (bs *blockStream) readString() (string, error) {
	size, err := bs.readCompactSize()
	if err != nil {
		return "", err
	}
	buf, err := bs.readBytes(int(size))
	if err != nil {
		return "", err
	}
	return string(buf), nil
}
