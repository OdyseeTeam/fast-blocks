package blockchain

import (
	"fast-blocks/blockchain/stream"
	"github.com/lbryio/lbry.go/v2/extras/errors"
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

type client struct {
	sync.Mutex
	blockFilesFound bool
	blockFilesGiven int
	blocksDir       string
	blockFiles      []string
}

type Chain interface {
	NextBlockFile(startingHeight int) (stream.Blocks, error)
}

type Config struct {
	BlocksDir string
}

func New(config Config) (Chain, error) {
	chain := &client{blocksDir: config.BlocksDir}
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
