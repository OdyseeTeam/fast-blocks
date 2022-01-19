package loader

import (
	"io"
	"path"

	"github.com/OdyseeTeam/fast-blocks/blockchain"
	"github.com/OdyseeTeam/fast-blocks/blockchain/model"
	"github.com/OdyseeTeam/fast-blocks/blockchain/stream"

	"github.com/cockroachdb/errors"
	"github.com/sirupsen/logrus"
)

var parallelFilesToLoad = 1

var nextFileToLoad chan int
var filesToLoad map[int]string

func LoadChain(chain blockchain.Chain, maxHeight int) error {
	var results = make(chan error)
	for i := 0; i < parallelFilesToLoad; i++ {
		go worker(i, chain, maxHeight, results)
	}

	for i := 0; i < parallelFilesToLoad; i++ {
		err := <-results
		if err != nil {
			logrus.Errorf("%+v", err)
		}
	}

	close(results)
	return nil
}

func worker(workerNum int, chain blockchain.Chain, maxHeight int, results chan<- error) {
	var err error
	var height int
	var fileNr int
Files:
	for {
		var blockStream stream.Blocks
		blockStream, err = chain.NextBlockFile(height)
		if err != nil {
			break
		}

		for {
			if blockStream == nil {
				println("finished processing files :) ")
				return // Need to go into a monitoring mode
			}

			var block *model.Block
			block, err = blockStream.NextBlock()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				break Files
			}

			height = block.Height
			if maxHeight > 0 && height > maxHeight {
				break Files
			}

			if height%1000 == 0 {
				logrus.Infof("Worker %d: file %s, block %dk", workerNum, path.Base(blockStream.BlockFile()), height/1000)
			}

			chain.Notify(*block)
		}
		fileNr++
	}
	results <- err
}
