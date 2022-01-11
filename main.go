package main

import (
	"os"

	"github.com/OdyseeTeam/fast-blocks/blockchain"
	"github.com/OdyseeTeam/fast-blocks/blockchain/model"
	"github.com/OdyseeTeam/fast-blocks/loader"
	"github.com/OdyseeTeam/fast-blocks/server"
	"github.com/OdyseeTeam/fast-blocks/storage"

	"github.com/sirupsen/logrus"
)

func main() {
	server.Start()
	storage.Start()
	chain, err := blockchain.New(blockchain.Config{BlocksDir: "/home/grin/.lbrycrd/blocks/"}) //, BlockFile: "blocks/blk00038.dat"})
	//chain, err := blockchain.New(blockchain.Config{BlocksDir: "./blocks/"})
	//chain, err := blockchain.New(blockchain.Config{BlocksDir: "./blocks/", BlockFile: "blocks/blk00038.dat"})
	if err != nil {
		logrus.Fatalf("%+v", err)
	}

	chain.OnOutput(func(vout model.Output) {
		println("Type: ", vout.ScriptType, " Address: ", vout.Address.Encoded, " Amount: ", vout.Amount)
		os.Exit(1)
	})

	err = loader.LoadChain(chain)
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
}
