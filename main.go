package main

import (
	"fmt"

	"github.com/OdyseeTeam/fast-blocks/lbrycrd"
	"github.com/OdyseeTeam/fast-blocks/storage"
	"github.com/sirupsen/logrus"
)

const deweysInLBC = 10 ^ 8 // 1 LBC is this many deweys
const blocksPerDay = 537   // 1 every 161 sec

func main() {
	//defer profile.Start(profile.MemProfile).Stop()
	//logrus.SetLevel(logrus.DebugLevel)

	storage.Start()

	//printStaleBlockHashes()

	BalanceSnapshots()
	//ClaimAddresses()
}

func ClaimAddresses() {

}

func printStaleBlockHashes() {
	blocks, err := lbrycrd.GetStaleBlockHashes()
	if err != nil {
		logrus.Errorf("%+v", err)
		return
	}

	for _, b := range blocks {
		fmt.Printf(`"%s": {},`+"\n", b)
	}
}
