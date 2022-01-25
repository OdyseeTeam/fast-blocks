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
	Workers int

	blockFilesMu sync.Mutex
	blockFiles   []blockAndHeight
	onBlockFn    func(block model.Block)
}

func New(dir string) (*Chain, error) {
	chain := &Chain{Workers: 1}
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
	return NewBlockFile(next)
}

func (c *Chain) OnBlock(fn func(model.Block)) {
	c.onBlockFn = fn
}

func (c *Chain) notify(block model.Block) {
	if c.onBlockFn != nil {
		c.onBlockFn(block)
	}
}

//Go reads the block data and passes it to the appropriate on* functions
func (c *Chain) Go(maxHeight int) error {
	fileChan := make(chan *BlockFile)

	if c.Workers < 1 {
		// could happen if you initialize an empty struct and forget to set this
		c.Workers = 1
	}
	logrus.Infof("running %d workers", c.Workers)

	wg := &sync.WaitGroup{}
	wg.Add(c.Workers)
	for i := 0; i < c.Workers; i++ {
		go func(i int) {
			defer wg.Done()
			c.worker(i, fileChan)
		}(i)
	}

	for {
		blockFile, err := c.nextBlockFile()
		if err != nil {
			return err
		}
		if blockFile == nil {
			break
		}

		if maxHeight > 0 && blockFile.firstHeight > uint64(maxHeight) {
			continue
		}

		fileChan <- blockFile
	}

	close(fileChan)
	wg.Wait()
	return nil
}

func (c *Chain) worker(workerNum int, blockFileChan chan *BlockFile) {
	for bf := range blockFileChan {
		logrus.Infof("worker %d starting block file %s", workerNum, bf.filename)
		blockCount := bf.firstHeight // TODO: blocks are not actually in order but this is close enough
		for {
			block, err := bf.NextBlock()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				logrus.Fatalf("block %d: %+v", blockCount, err) // TODO: handle this better. tho tbh, refactor blockstream would be better
				return
			}

			if blockCount%10000 == 0 {
				logrus.Infof("Worker %d: file %s, block %dk", workerNum, path.Base(bf.Filename()), blockCount/1000)
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
