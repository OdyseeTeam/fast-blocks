package blockchain

import (
	"fast-blocks/blockchain/model"
	"fast-blocks/blockchain/stream"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lbryio/lbry.go/v2/extras/errors"
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var GenesisHash = chainhash.Hash([chainhash.HashSize]byte{ // Make go vet happy.
	0x9c, 0x89, 0x28, 0x3b, 0xa0, 0xf3, 0x22, 0x7f,
	0x6c, 0x03, 0xb7, 0x02, 0x16, 0xb9, 0xf6, 0x65,
	0xf0, 0x11, 0x8d, 0x5e, 0x0f, 0xa7, 0x29, 0xce,
	0xdf, 0x4f, 0xb3, 0x4d, 0x6a, 0x34, 0xf4, 0x63,
})

// MainNetParams define the lbrycrd network. See https://github.com/lbryio/lbrycrd/blob/master/src/chainparams.cpp
var MainNetParams = chaincfg.Params{
	PubKeyHashAddrID: 0x55,
	ScriptHashAddrID: 0x7a,
	PrivateKeyID:     0x1c,
	Bech32HRPSegwit:  "lbc",
	//WitnessPubKeyHashAddrID: , // i cant find these in bitcoin codebase either
	//WitnessScriptHashAddrID:,
	GenesisHash:   &GenesisHash,
	Name:          "mainnet",
	Net:           wire.BitcoinNet(0xfae4aaf1),
	DefaultPort:   "9246",
	BIP0034Height: 1,
	BIP0065Height: 200000,
	BIP0066Height: 200000,
}

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

var blockFileRE = regexp.MustCompile(`.+/blk[0-9]*\.dat`)

func (c *client) loadBlockFiles() error {
	var files []string
	println(os.Getwd())
	err := filepath.Walk(c.blocksDir, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && blockFileRE.MatchString(path) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return errors.Err(err)
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
