package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"sync"

	"github.com/OdyseeTeam/fast-blocks/blockchain"
	"github.com/OdyseeTeam/fast-blocks/blockchain/model"
	"github.com/OdyseeTeam/fast-blocks/lbrycrd"
	"github.com/OdyseeTeam/fast-blocks/storage"
	"github.com/lbryio/lbcd/txscript"
	"github.com/lbryio/lbry.go/v3/schema/stake"
	"github.com/pkg/profile"
	"github.com/sirupsen/logrus"
)

const deweysInLBC = 10 ^ 8 // 1 LBC is this many deweys
const blocksPerDay = 537   // 1 every 161 sec

func main() {
	defer profile.Start(profile.MemProfile).Stop()
	//logrus.SetLevel(logrus.DebugLevel)

	storage.Start()

	//printStaleBlockHashes()

	BalanceSnapshots(66000)
	//ClaimAddresses()
}

func ClaimAddresses() {
	//look for addresses where sends TO them are def utility
	//same for sends FROM
	//
	//parse chain, store all claim addresses and all addresses that supports were sent to (shoudl be same?)
	//
	//group by size and by number of txns

	maxHeight := 0 // 0 = load it all

	chain, err := blockchain.New("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	chain.ParallelFilesToLoad = 50

	addressChan := make(chan []byte)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		addressCollector(addressChan)
		wg.Done()
	}()

	chain.OnBlock(func(block model.Block) {
		if _, ok := lbrycrd.StaleBlockHashes[block.Header.BlockHash.String()]; ok {
			return
		}

		logrus.Debugf("BLOCK %d (%s)", block.Height, block.Header.BlockHash)
		for _, tx := range block.Transactions {
			logrus.Debugf("    TX %s", tx.Hash)
			hasConsumptiveUse := false
			for n, out := range tx.Outputs {
				if out.ClaimScript != nil {
					hasConsumptiveUse = true
					addressChan <- out.Address.ScriptAddress()

					switch out.ClaimScript.Opcode {
					case txscript.OP_CLAIMNAME, txscript.OP_UPDATECLAIM:
						h, err := stake.DecodeClaimBytes(out.ClaimScript.Value, "")
						if err != nil {
							logrus.Errorf("could not unmarshall value in %s:%d", out.TransactionHash, n)
							logrus.Errorln(hex.EncodeToString(out.ClaimScript.Value))
						} else if !h.IsClaim() {
							logrus.Errorf("unmarshall fine but its not a claim in %s:%d", out.TransactionHash, n)
							logrus.Errorln(hex.EncodeToString(out.ClaimScript.Value))
						} else if s := h.Claim.GetStream(); s != nil {
							addressChan <- s.GetFee().GetAddress()
						}
					case txscript.OP_SUPPORTCLAIM:
					default:
						logrus.Errorf("unknown opcode in %s:%d", out.TransactionHash, n)
					}
				}
				if out.Purchase != nil {
					hasConsumptiveUse = true

				}

				logrus.Debugf("        OUT %d -> %s (%d)", n, out.Address, out.Amount)
			}

			if !hasConsumptiveUse {
				continue
			}

			// if tx has a claim, then inputs are legit user addresses
			//for _, in := range tx.Inputs {
			// get address for this input and store it
			//}
		}
	})

	chain.Go(maxHeight)

	close(addressChan)
	wg.Wait()

	logrus.Printf("done")

}

func addressCollector(addrChan chan []byte) {
	addresses := make(map[string]struct{})

	for a := range addrChan {
		addresses[hex.EncodeToString(a)] = struct{}{}
	}

	logrus.Printf("saving addresses to file")

	f, err := os.Create(fmt.Sprintf("consumptive_addresses"))
	if err != nil {
		logrus.Error(err)
		return
	}
	defer f.Close()

	for a := range addresses {
		f.WriteString(a + "\n")
	}

	logrus.Printf("collected %d addresses", len(addresses))
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
