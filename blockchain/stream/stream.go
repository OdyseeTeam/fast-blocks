package stream

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fast-blocks/blockchain/model"
	"fast-blocks/blockchain/script"
	"fast-blocks/lbrycrd"
	"fast-blocks/storage"
	"fast-blocks/util"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/lbryio/lbry.go/v2/extras/errors"
	"github.com/lbryio/lbry.go/v2/schema/stake"
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"time"
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
			return nil, errors.Err(err)
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
		block.Transactions = append(block.Transactions, t.Hash)
	}

	return block, errors.Err(storage.DB.Exec(`INSERT INTO blocks VALUES ?`, &block))
}

var magicNumberConst = []byte{250, 228, 170, 241}

func (bs *blockStream) setBlockInfo(block *model.Block) error {
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
		readBuf = append(readBuf, buf...)
		return binary.LittleEndian.Uint64(buf), readBuf, nil
	}

	return 0, nil, errors.Err("size is greater than 255")
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

		tx.Hash = chainhash.DoubleHashH(txBytes).String()
		tx.BlockHash = block.BlockHash
		for _, o := range outputs {
			o.TransactionHash = tx.Hash
			o.BlockHash = block.BlockHash
			err := storage.DB.Exec(`INSERT INTO outputs VALUES ?`, &o)
			if err != nil {
				return nil, errors.Err(err)
			}
		}
		for _, i := range inputs {
			i.TransactionHash = tx.Hash
			i.BlockHash = block.BlockHash
			err := storage.DB.Exec(`INSERT INTO inputs VALUES ?`, &i)
			if err != nil {
				return nil, errors.Err(err)
			}
		}

		tx.LockTime = time.Unix(int64(lockTimeBytes), 0)

		err = storage.DB.Exec(`INSERT INTO transactions VALUES ?`, &tx)
		if err != nil {
			return nil, errors.Err(err)
		}

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
		in.TxRef = "Coinbase"
		if !isCoinBase(buf) {
			in.TxRef = hex.EncodeToString(util.ReverseBytes(buf))
		}

		in.Position, buf, err = bs.readUint32R()
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
		in.Script = script.ToHex(scriptBytes)

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
		pk, _ := txscript.ParsePkScript(scriptBytes)
		scriptType := lbrycrd.GetPublicKeyScriptType(scriptBytes)
		if pk.Class() != txscript.NonStandardTy {
			address := lbrycrd.GetAddressFromPublicKeyScript(scriptBytes)
			out.Address = model.Address{Encoded: address}
			out.PKScript = scriptBytes
			out.ScriptType = scriptType
		} else if pk.Class() == txscript.NonStandardTy {
			if lbrycrd.IsClaimScript(scriptBytes) {
				txscript.NewScriptBuilder()
				if lbrycrd.IsClaimNameScript(scriptBytes) {
					name, value, pkscript, err := lbrycrd.ParseClaimNameScript(scriptBytes)
					if err != nil {
						return nil, nil, err
					}
					if false {
						println("Name: ", name)
					}
					_, err = stake.DecodeClaimBytes(value, "lbrycrd_main")
					if err != nil {
						logrus.Error(err)
						continue
					}
					addy := lbrycrd.GetAddressFromPublicKeyScript(pkscript)
					if err != nil {
						return nil, nil, err
					}
					out.Address = model.Address{Encoded: addy}
					//println(helper.Claim.String())
					//err = storage.DB.Exec(`INSERT INTO claims VALUES ?`, &helper.Claim)
					//if err != nil {
					//	return nil, nil, errors.Err(err)
					//}
				}
			} else if lbrycrd.IsPurchaseScript(scriptBytes) {
				purchase, err := lbrycrd.ParsePurchaseScript(scriptBytes)
				if err != nil {
					return nil, nil, err
				}
				if false {
					println("Purchase: ", purchase.ClaimHash)
				}

			} else {
				if false {
					println(txscript.DisasmString(scriptBytes))
					println("Non claim, no standard transaction")
				}
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
