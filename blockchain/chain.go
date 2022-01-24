package blockchain

import (
	"encoding/binary"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/OdyseeTeam/fast-blocks/blockchain/model"

	"github.com/cockroachdb/errors"
	"github.com/sirupsen/logrus"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type Chain struct {
	ParallelFilesToLoad int

	blockFilesMu sync.Mutex
	blockFiles   []blockAndHeight

	onBlockFn       func(block model.Block)
	onTransactionFn func(transaction model.Transaction)
	onInputFn       func(input model.Input)
	onOutputFn      func(output model.Output)
}

func New(dir string) (*Chain, error) {
	chain := &Chain{ParallelFilesToLoad: 1}
	err := chain.loadBlockFiles(dir)
	if err != nil {
		return nil, err
	}
	return chain, nil
}

func (c *Chain) loadBlockFiles(dir string) error {
	var err error
	c.blockFiles, err = blockFilesOrderedByHeight(dir)
	return err
}

func (c *Chain) nextBlockFile() (*BlockFile, error) {
	if len(c.blockFiles) == 0 {
		return nil, nil
	}

	c.blockFilesMu.Lock()
	defer c.blockFilesMu.Unlock()

	next := c.blockFiles[0]
	c.blockFiles = c.blockFiles[1:]
	logrus.Infof("Starting block file %s", next.filename)
	return NewBlockFile(next)
}

func (c *Chain) OnBlock(fn func(model.Block)) {
	c.onBlockFn = fn
}

func (c *Chain) OnTransaction(fn func(model.Transaction)) {
	c.onTransactionFn = fn
}

func (c *Chain) OnInput(fn func(model.Input)) {
	c.onInputFn = fn
}

func (c *Chain) OnOutput(fn func(model.Output)) {
	c.onOutputFn = fn
}

func (c *Chain) notify(block model.Block) {
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

//Go reads the block data and passes it to the appropriate on* functions
func (c *Chain) Go(maxHeight int) {
	wg := sync.WaitGroup{}
	wg.Add(c.ParallelFilesToLoad)
	for i := 0; i < c.ParallelFilesToLoad; i++ {
		go func() {
			c.worker(i, uint64(maxHeight))
			wg.Done()
		}()
	}
	wg.Wait()
}

func (c *Chain) worker(workerNum int, maxHeight uint64) {
	for {
		blockStream, err := c.nextBlockFile()
		if err != nil {
			logrus.Fatal(err) // TODO: handle this better. tho tbh, refactor blockstream would be better
			return
		}

		blockCount := blockStream.firstHeight // blocks are not strictly in order but this is close enough
		for {
			if blockStream == nil {
				return
			}

			block, err := blockStream.NextBlock()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				logrus.Fatalf("block %d: %+v", blockCount, err) // TODO: handle this better. tho tbh, refactor blockstream would be better
				return
			}

			if maxHeight > 0 && blockCount > maxHeight {
				return
			}

			if blockCount%1000 == 0 {
				logrus.Infof("Worker %d: file %s, block %dk", workerNum, path.Base(blockStream.Filename()), blockCount/1000)
			}

			c.notify(*block)
			blockCount++
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

type blockAndHeight struct {
	filename    string
	firstHeight uint64
}

// blockFilesOrderedByHeight returns a slice of block files, ordered by the height of the first
// block in the file
func blockFilesOrderedByHeight(blocksDir string) ([]blockAndHeight, error) {
	var blockFiles []blockAndHeight

	blocksDir = strings.TrimSuffix(blocksDir, "/")

	db, err := leveldb.OpenFile(blocksDir+"/index", nil)
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

		blockFiles = append(blockFiles, blockAndHeight{
			filename:    blocksDir + "/" + filename,
			firstHeight: firstHeight,
		})
	}
	iter.Release()

	err = iter.Error()
	if err != nil {
		return nil, errors.Wrap(err, "")
	}

	sort.Slice(blockFiles, func(i, j int) bool {
		return blockFiles[i].firstHeight < blockFiles[j].firstHeight
	})

	return blockFiles, nil
}
