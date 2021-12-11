package main

import (
	"fast-blocks/blockchain"
	"fast-blocks/loader"
	"github.com/lbryio/lbry.go/v2/extras/errors"
	"github.com/sirupsen/logrus"
)

func main() {
	chain, err := blockchain.New(blockchain.Config{BlocksDir: "./blocks/"}) //, BlockFile: "blocks/blk00034.dat"})
	if err != nil {
		logrus.Fatal(errors.FullTrace(err))
	}
	err = loader.LoadChain(chain)
	if err != nil {
		logrus.Error(errors.FullTrace(err))
	}
}
