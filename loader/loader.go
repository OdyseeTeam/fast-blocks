package loader

import (
	"fast-blocks/blockchain"
	"fast-blocks/blockchain/model"
	"fast-blocks/blockchain/stream"
	"github.com/lbryio/lbry.go/v2/extras/errors"
	"github.com/sirupsen/logrus"
	"io"
)

var parallelFilesToLoad = 50

var nextFileToLoad chan int
var filesToLoad map[int]string

func LoadChain(chain blockchain.Chain) error {
	var results = make(chan error)
	for i := 0; i < parallelFilesToLoad; i++ {
		go startLoadWorker(i, chain, results)
	}
	for i := 0; i < parallelFilesToLoad; i++ {
		err := <-results
		if err != nil {
			logrus.Error(errors.FullTrace(err))
		}
	}
	close(results)
	return nil
}

func startLoadWorker(worker int, chain blockchain.Chain, results chan<- error) {
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
				return // Need to go into a minitoring mode
			}
			var block *model.Block
			block, err = blockStream.NextBlock()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				break Files
			}
			chain.Notify(*block)
			height = block.Height
			if height%1000 == 0 {
				logrus.Info("Worker: ", worker, " Blockfile: ", blockStream.BlockFile(), ", Block Nr: ", height, " Txs: ", len(block.Transactions))
			}

		}
		fileNr++
	}
	results <- err
}
