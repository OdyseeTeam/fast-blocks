package main

import (
	"fast-blocks/blockchain"
	"fast-blocks/blockchain/model"
	"fast-blocks/loader"
	"fast-blocks/server"
	"fast-blocks/storage"
	"github.com/lbryio/lbry.go/v2/extras/errors"
	"github.com/sirupsen/logrus"
)

func main() {
	server.Start()
	storage.Start()
	chain, err := blockchain.New(blockchain.Config{BlocksDir: "/home/odysee/fast-blocks/blocks/"}) //, BlockFile: "blocks/blk00038.dat"})
	// chain, err := blockchain.New(blockchain.Config{BlocksDir: "./blocks/", BlockFile: "blocks/blk00038.dat"})
	if err != nil {
		logrus.Fatal(errors.FullTrace(err))
	}
	chain.OnOutput(func(vout *model.Output) {
		println("Type: ", vout.ScriptType, " Address: ", vout.Address.Encoded, " Amount: ", vout.Amount)
	})
	err = loader.LoadChain(chain)
	if err != nil {
		logrus.Error(errors.FullTrace(err))
	}
}
