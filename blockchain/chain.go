package blockchain

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/OdyseeTeam/fast-blocks/blockchain/model"
	"github.com/OdyseeTeam/fast-blocks/blockchain/stream"

	"github.com/cockroachdb/errors"
	"github.com/sirupsen/logrus"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type client struct {
	sync.Mutex
	blockFilesFound bool
	blockFilesGiven int
	blocksDir       string
	blockFile       string
	blockFiles      []string

	onBlockFn       func(block model.Block)
	onTransactionFn func(transaction model.Transaction)
	onInputFn       func(input model.Input)
	onOutputFn      func(output model.Output)
}

type Chain interface {
	NextBlockFile(startingHeight int) (stream.Blocks, error)
	OnBlock(func(block model.Block))
	OnTransaction(func(transaction model.Transaction))
	OnInput(func(input model.Input))
	OnOutput(func(output model.Output))
	Notify(block model.Block)
}

type Config struct {
	BlocksDir string
	BlockFile string
}

func New(config Config) (Chain, error) {
	chain := &client{blocksDir: config.BlocksDir, blockFile: config.BlockFile}
	err := chain.loadBlockFiles()
	if err != nil {
		return nil, err
	}
	return chain, nil
}

func (c *client) NextBlockFile(startingHeight int) (stream.Blocks, error) {
	if len(c.blockFiles) == 0 {
		return nil, nil
	}
	if c.blockFile != "" {
		for _, file := range c.blockFiles {
			if file == c.blockFile {
				c.blockFiles = []string{}
				return stream.New(file, c.blockFilesGiven, startingHeight, nil)
			}
		}
	}
	c.Lock()
	defer c.Unlock()
	next := c.blockFiles[0]
	c.blockFiles = c.blockFiles[1:]
	logrus.Info("Starting block file: ", next)
	c.blockFilesGiven++
	return stream.New(next, c.blockFilesGiven, startingHeight, nil)
}

func (c *client) loadBlockFiles() error {
	files, err := blockFilesOrderedByHeight(strings.TrimSuffix(c.blocksDir, "/") + "/index")
	if err != nil {
		return err
	}

	for i := range files {
		files[i] = strings.TrimSuffix(c.blocksDir, "/") + "/" + files[i]
	}

	c.blockFiles = files
	return nil
}

func (c *client) OnBlock(fn func(model.Block)) {
	c.onBlockFn = fn
}

func (c *client) OnTransaction(fn func(model.Transaction)) {
	c.onTransactionFn = fn
}

func (c *client) OnInput(fn func(model.Input)) {
	c.onInputFn = fn
}

func (c *client) OnOutput(fn func(model.Output)) {
	c.onOutputFn = fn
}

func (c *client) Notify(block model.Block) {
	if c.onBlockFn != nil {
		c.onBlockFn(block)
	}

	for _, tx := range block.Transactions {
		if c.onTransactionFn != nil {
			c.onTransactionFn(tx)
		}
		if c.onOutputFn != nil {
			for _, out := range tx.Outputs {
				c.onOutputFn(out)
			}
		}
		if c.onInputFn != nil {
			for _, in := range tx.Inputs {
				c.onInputFn(in)
			}
		}
	}

}

// https://bitcoin.stackexchange.com/questions/67515/format-of-a-block-keys-contents-in-bitcoinds-leveldb
// binary.Uvarint is supposed to do this, except it doesn't.
// And it is not the same varint as in bitcoin serialized varints.
// this is ported from "The Undocumented Internals of The Bitcoin
// and Ethereum Blockchains" python code
func base128(b []byte, offset uint64) (uint64, uint64) {
	for n := 0; ; n++ {
		ch := int(b[offset])
		offset++
		n = (n << 7) | (ch & 0x7f)
		if ch&0x80 != 128 {
			return uint64(n), offset
		}
	}
	return 0, 0
}

// blockFilesOrderedByHeight returns a slice of block files, ordered by the height of the first
// block in the file
func blockFilesOrderedByHeight(indexdbPath string) ([]string, error) {
	type blockInfo struct {
		filename    string
		firstHeight uint64
	}
	var blockInfos []blockInfo

	db, err := leveldb.OpenFile(indexdbPath, nil)
	defer db.Close()

	iter := db.NewIterator(util.BytesPrefix([]byte("f")), nil)
	for iter.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := iter.Key()
		value := iter.Value()

		blockFileNum := binary.LittleEndian.Uint32(key[1:])
		filename := fmt.Sprintf("blk%05d.dat", blockFileNum)

		// references
		// https://bitcoin.stackexchange.com/questions/67515/format-of-a-block-keys-contents-in-bitcoinds-leveldb
		// https://bitcoin.stackexchange.com/q/28168/616
		// https://github.com/bitcoin/bitcoin/blob/fcbc8bfa6d10cac4f16699d6e6e68fb6eb98acd0/src/main.h#L392
		// https://github.com/alecalve/python-bitcoin-blockchain-parser

		var (
			offset uint64
			//blocks      uint64
			//size        uint64
			//undoSize    uint64
			firstHeight uint64
			//lastHeight  uint64
			//firstTime   uint64
			//lastTime    uint64
		)
		_, offset = base128(value, offset)
		_, offset = base128(value, offset)
		_, offset = base128(value, offset)
		firstHeight, offset = base128(value, offset)
		//lastHeight, offset = base128(value, offset)
		//firstTime, offset = base128(value, offset)
		//lastTime, offset = base128(value, offset)

		blockInfos = append(blockInfos, blockInfo{filename: filename, firstHeight: firstHeight})
	}
	iter.Release()

	err = iter.Error()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	sort.Slice(blockInfos, func(i, j int) bool {
		return blockInfos[i].firstHeight < blockInfos[j].firstHeight
	})

	filenames := make([]string, len(blockInfos))
	for n, b := range blockInfos {
		filenames[n] = b.filename
	}
	return filenames, nil
}
