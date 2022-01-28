package chain

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/lbryio/lbcd/chaincfg"
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/txscript"
	"github.com/sirupsen/logrus"
	"github.com/valyala/bytebufferpool"
	"golang.org/x/crypto/ripemd160"
)

type BlockFile struct {
	filename    string
	firstHeight uint64

	file       *os.File
	closed     bool
	currHeight uint64
}

func (bf BlockFile) Filename() string {
	return bf.filename
}

func (bf *BlockFile) Offset() int64 {
	offset, err := bf.file.Seek(0, io.SeekCurrent)
	if err != nil {
		logrus.Fatalf("%+v", errors.Wrap(err, "file offset"))
	}
	return offset
}

func (bf *BlockFile) Close() error {
	if bf.closed {
		return nil
	}

	bf.closed = true

	err := bf.file.Close()
	return errors.Wrap(err, "")
}

func (bf *BlockFile) NextBlock() (*Block, error) {
	var err error

	if bf.closed {
		return nil, errors.New("blockfile closed")
	}

	if bf.file == nil {
		bf.file, err = os.OpenFile(bf.filename, os.O_RDONLY, 0)
		if err != nil {
			return nil, errors.Wrap(err, "opening block file")
		}
	}

	if bf.currHeight == 0 {
		bf.currHeight = bf.firstHeight
	}

	err = consumeUntilNextBlock(bf.file)
	if err != nil {
		return nil, errors.WithMessagef(err, "file %s, offset %d", bf.filename, bf.Offset())
	}

	blockSize, err := readUint32(bf.file)
	if err != nil {
		return nil, err
	}

	startOffset := bf.Offset()

	block, err := readBlock(bf.file)
	if err != nil {
		return nil, err
	}

	bytesRead := bf.Offset() - startOffset
	if bytesRead != int64(blockSize) {
		return nil, errors.Newf("expected block size to be %d, but read %d bytes", blockSize, bytesRead)
	}

	block.Size = blockSize

	// TODO: this is wrong. the blocks are not stored in order in the file
	// use the leveldb index if you want to read the blocks in order
	// see blockReader.blockFilesOrderedByHeight() for a starting point
	block.Height = bf.currHeight
	bf.currHeight++

	return block, nil
}

func readBlock(r io.Reader) (*Block, error) {
	var err error
	block := &Block{}

	// TODO: use lbcutil.NewBlockFromBytes

	block.Header, err = readHeader(r)
	if err != nil {
		return nil, err
	}

	txCnt, err := readCompactSize(r)
	if err != nil {
		return nil, err
	}

	block.Transactions, err = readTransactions(r, int(txCnt))
	if err != nil {
		return nil, err
	}

	return block, nil
}

func readTransactions(r io.Reader, txCount int) ([]Transaction, error) {
	var err error
	transactions := make([]Transaction, txCount)

	// only useful if running many parallel threads
	txBytes := bytebufferpool.Get() // TODO: benchmark compare memory usage
	defer bytebufferpool.Put(txBytes)
	//txBytes := new(bytes.Buffer)

	for i := 0; i < txCount; i++ {
		tx := Transaction{}

		txBytes.Reset()
		txReader := io.TeeReader(r, txBytes)

		tx.Version, err = readUint32(txReader)
		if err != nil {
			return nil, err
		}

		// reading from r instead of txReader because we don't know if we're about to
		// read the inputCount or the segwit marker
		// txid:   doubleSHA([nVersion][txins][txouts][nLockTime])
		// wtxid:  doubleSHA([nVersion][marker][flag][txins][txouts][witness][nLockTime])
		// https://en.bitcoin.it/wiki/BIP_0141#Transaction_ID
		inputCountOrMarker, err := readCompactSize(r)
		if err != nil {
			return nil, err
		}

		if inputCountOrMarker == 0 {
			// if 0 inputs, then what we actually read was the marker
			tx.IsSegWit = true

			flag, err := readByte(r)
			if err != nil {
				return nil, err
			}
			if flag != 0x01 {
				logrus.Fatal("marker (zero inputs) detected but flag is invalid")
			}

			tx.InputCount, err = readCompactSize(txReader)
			if err != nil {
				return nil, err
			}
		} else {
			tx.InputCount = inputCountOrMarker
			// write the size back to txbytes so our tx hash is correct
			err = writeVarInt(txBytes, tx.InputCount)
			if err != nil {
				return nil, err
			}
		}

		tx.Inputs, err = readInputs(txReader, int(tx.InputCount))
		if err != nil {
			return nil, err
		}

		tx.OutputCount, err = readCompactSize(txReader)
		if err != nil {
			return nil, err
		}

		tx.Outputs, err = readOutputs(txReader, int(tx.OutputCount))
		if err != nil {
			return nil, err
		}

		if tx.IsSegWit {
			// dont use txreader because witness data is not part of txid
			// https://en.bitcoin.it/wiki/BIP_0141#Transaction_ID
			for i := 0; i < int(tx.InputCount); i++ {
				witnessCount, err := readCompactSize(r)
				if err != nil {
					return nil, err
				}

				tx.Witnesses = make([]Witness, witnessCount)

				for i := 0; i < int(witnessCount); i++ {
					size, err := readCompactSize(r)
					if err != nil {
						return nil, err
					}

					tx.Witnesses[i], err = read(r, int(size))
					if err != nil {
						return nil, err
					}
				}
			}
		}

		lockTimeBytes, err := readUint32(txReader)
		if err != nil {
			return nil, err
		}
		tx.LockTime = time.Unix(int64(lockTimeBytes), 0)

		txHash := chainhash.DoubleHashH(txBytes.Bytes())
		tx.Hash = &txHash
		transactions[i] = tx
	}

	return transactions, nil
}

func readInputs(r io.Reader, inputCount int) ([]Input, error) {
	var err error
	var inputs []Input
	for i := 0; i < inputCount; i++ {
		var buf []byte
		in := Input{}

		buf, err = read(r, chainhash.HashSize)
		if err != nil {
			return nil, err
		}
		in.PrevTxHash, err = chainhash.NewHash(buf)
		if err != nil {
			return nil, err
		}

		in.PrevTxIndex, err = readUint32(r)
		if err != nil {
			return nil, err
		}

		scriptLength, err := readCompactSize(r)
		if err != nil {
			return nil, err
		}

		scriptBytes, err := read(r, int(scriptLength))
		if err != nil {
			return nil, err
		}
		in.Script = scriptBytes

		in.Sequence, err = readUint32R(r)
		if err != nil {
			return nil, err
		}

		inputs = append(inputs, in)
	}
	return inputs, nil
}

func readOutputs(r io.Reader, outputCount int) ([]Output, error) {
	var err error
	var outputs []Output
	for i := 0; i < outputCount; i++ {
		out := Output{}
		out.Amount, err = readUint64(r)
		if err != nil {
			return nil, err
		}

		scriptLength, err := readCompactSize(r)
		if err != nil {
			return nil, err
		}

		scriptBytes, err := read(r, int(scriptLength))
		if err != nil {
			return nil, err
		}
		out.PKScript = scriptBytes
		//logrus.Traceln(txscript.DisasmString(scriptBytes))

		scriptClass, addresses, _, err := txscript.ExtractPkScriptAddrs(scriptBytes, &chaincfg.MainNetParams)
		if err != nil {
			return nil, err
		}
		out.ScriptClass = scriptClass

		if scriptClass != txscript.NonStandardTy {
			if len(addresses) == 1 {
				out.Address = addresses[0]
			}

			claimScript, err := txscript.ExtractClaimScript(scriptBytes)
			if err != nil {
				claimScript = nil
			}

			if claimScript != nil {
				out.ClaimScript = claimScript
			} else if IsPurchaseScript(scriptBytes) {
				purchase, err := parsePurchaseScript(scriptBytes)
				if err != nil {
					return nil, err
				}
				logrus.Debugln("Purchase: ", purchase.ClaimHash)
				out.Purchase = purchase
			}
		}

		outputs = append(outputs, out)
	}

	return outputs, nil
}

const blockHeaderLength = 112

func readHeader(r io.Reader) (*Header, error) {
	header := &Header{}

	headerBytes, err := read(r, blockHeaderLength)
	if err != nil {
		return nil, err
	}

	blockHash := chainhash.DoubleHashH(headerBytes)
	header.BlockHash = &blockHash

	header.Version = binary.LittleEndian.Uint32(headerBytes[0:4])
	if header.Version > 1 && header.Version != 536870912 && header.Version != 536870913 {
		return nil, errors.New("version should always be 1 or 536870912,536870913")
	}

	header.PrevBlockHash, err = chainhash.NewHash(ReverseBytes(headerBytes[4:36]))
	if err != nil {
		return nil, errors.Wrap(err, "")
	}

	header.MerkleRoot = headerBytes[36:68]
	header.ClaimTrieRoot = headerBytes[68:100]
	header.TimeStamp = time.Unix(int64(binary.LittleEndian.Uint32(headerBytes[100:104])), 0)
	header.Bits = binary.LittleEndian.Uint32(headerBytes[104:108])
	header.Nonce = binary.LittleEndian.Uint32(headerBytes[108:112])

	return header, nil
}

func readCompactSize(r io.Reader) (uint64, error) {
	size, err := readByte(r)
	if err != nil {
		return 0, err
	}

	switch size {
	case 0xff:
		return readUint64(r)
	case 0xfe:
		varInt, err := readUint32(r)
		return uint64(varInt), err
	case 0xfd:
		varInt, err := readUint16(r)
		return uint64(varInt), err
	default:
		return uint64(size), nil
	}
}

func writeVarInt(w io.Writer, v uint64) error {
	if v < 0xfd { // single byte
		_, err := w.Write([]byte{byte(v)})
		return errors.Wrap(err, "")
	} else if v <= 0xffff { // uint16
		_, err := w.Write([]byte{
			0xfd,
			byte(v), byte(v >> 8),
		})
		return errors.Wrap(err, "")
	} else if v <= 0xffffff { // uint32
		_, err := w.Write([]byte{
			0xfe,
			byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
		return errors.Wrap(err, "")
	} else { // uint64
		_, err := w.Write([]byte{
			0xff,
			byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24),
			byte(v >> 32), byte(v >> 40), byte(v >> 48), byte(v >> 56),
		})
		return errors.Wrap(err, "")
	}
}

// https://learnmeabitcoin.com/technical/magic-bytes
var magicBytes = []byte{0xfa, 0xe4, 0xaa, 0xf1}

// consumeUntilNextBlock consumes 0x00 bytes until it finds the next set of magic bytes
// looks like the .blk files sometimes just have stretches of 0s in them...
func consumeUntilNextBlock(r io.Reader) error {
	var firstByte byte
	var err error

	b, err := read(r, len(magicBytes))
	if err != nil {
		return err
	} else if bytes.Equal(b, magicBytes) {
		// exit fast for the most common case
		return nil
	}

	if !bytes.Equal(b, []byte{0, 0, 0, 0}) {
		return errors.Newf("expected magic bytes %s, got %s", hex.EncodeToString(magicBytes), hex.EncodeToString(b))
	}

	// continue consuming the 0x00 bytes one by one
	for {
		firstByte, err = readByte(r)
		if err != nil {
			return err
		}

		if firstByte != 0x00 {
			break
		}
	}

	// after getting through all of the 0x00 bytes, check again for magic bytes
	rest, err := read(r, len(magicBytes)-1)
	if err != nil {
		return err
	}

	if firstByte != magicBytes[0] || !bytes.Equal(magicBytes[1:], rest) {
		return errors.Newf("expected magic bytes %s, got %s", hex.EncodeToString(magicBytes),
			hex.EncodeToString(append([]byte{firstByte}, rest...)))
	}

	return nil
}

func readUint64(r io.Reader) (uint64, error) {
	buf, err := read(r, 8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buf), nil
}

func readUint32(r io.Reader) (uint32, error) {
	buf, err := read(r, 4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf), nil
}

func readUint32R(r io.Reader) (uint32, error) {
	buf, err := read(r, 4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(ReverseBytes(buf)), nil
}

func readUint16(r io.Reader) (uint16, error) {
	buf, err := read(r, 2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(buf), nil
}

func readByte(r io.Reader) (byte, error) {
	buf, err := read(r, 1)
	if err != nil {
		return 0, err
	}
	return buf[0], nil
}

func read(r io.Reader, numBytes int) ([]byte, error) {
	b := make([]byte, numBytes)
	n, err := r.Read(b)

	if err != nil {
		return nil, errors.Wrap(err, "read")
	}

	if n < numBytes {
		return nil, errors.Newf("expected to read %d bytes, only got %d", numBytes, n)
	}

	return b, nil
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
