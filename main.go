package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"sync"

	"github.com/OdyseeTeam/fast-blocks/blockchain"
	"github.com/OdyseeTeam/fast-blocks/blockchain/model"
	"github.com/OdyseeTeam/fast-blocks/blockchain/stream"
	"github.com/OdyseeTeam/fast-blocks/lbrycrd"
	"github.com/OdyseeTeam/fast-blocks/loader"
	"github.com/OdyseeTeam/fast-blocks/storage"
	"github.com/sirupsen/logrus"
)

var utxos map[string]*utxo
var predeleted map[string]bool

const deweysInLBC = 10 ^ 8 // 1 LBC is this many deweys
const blocksPerDay = 537   // 1 every 161 sec

var reportBlocks = map[int]struct{}{
	37000:   {}, // 2016 q3
	103000:  {}, // 2016 q4
	150900:  {}, // 2016 q1
	200000:  {}, // 2017 q2
	249000:  {}, // 2017 q3
	298000:  {}, // jan 1 2018
	347000:  {}, // apr 1 2018
	396000:  {},
	445000:  {},
	495000:  {}, // dec 31 2018
	544000:  {}, // apr 1 2019
	593000:  {},
	642000:  {},
	692000:  {}, // jan 1 2020
	741000:  {},
	790000:  {},
	839500:  {}, // 2020 q3
	889500:  {}, // jan 1 2021
	938000:  {},
	988000:  {},
	1038000: {},
	1088000: {}, // jan 1 2022
}

func main() {
	logrus.SetLevel(logrus.DebugLevel)
	storage.Start()

	//printStaleBlockHashes()

	BalanceSnapshots()
}

func BalanceSnapshots() {
	maxHeight := 0 // 0 = load it all

	//spew.Dump(lbrycrd.GetStaleBlocks())
	//return

	utxos = make(map[string]*utxo)
	predeleted = make(map[string]bool)

	chain, err := blockchain.New(blockchain.Config{BlocksDir: "/home/grin/.lbrycrd-17.3.3/blocks/"})
	if err != nil {
		logrus.Fatalf("%+v", err)
	}

	actions := make(chan spendOrCreate)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		accountant(actions)
		wg.Done()
	}()

	chain.OnBlock(func(block model.Block) {
		if _, ok := lbrycrd.StaleBlockHashes[block.BlockHash.String()]; ok {
			return
		}

		logrus.Debugf("BLOCK %d (%s)", block.Height, block.BlockHash)
		for _, tx := range block.Transactions {
			logrus.Debugf("    TX %s", tx.Hash)
			for _, in := range tx.Inputs {
				// inputs spend UTXOs
				if in.TxRef == stream.CoinbaseRef {
					logrus.Debugf("        IN coinbase")
					continue
				}
				logrus.Debugf("        IN  %s:%d", in.TxRef, in.Position)
				actions <- spendOrCreate{spend: &outpoint{txid: in.TxRef, nout: in.Position}}
			}
			for n, out := range tx.Outputs {
				logrus.Debugf("        OUT %d -> %s (%d)", n, out.Address.Encoded, out.Amount)
				// outputs create UTXOs
				actions <- spendOrCreate{create: &utxo{
					outpoint: outpoint{txid: tx.Hash.String(), nout: uint32(n)},
					balance:  balance{address: out.Address.Encoded, amount: out.Amount},
				}}
			}
		}

		if _, ok := reportBlocks[block.Height]; ok {
			actions <- spendOrCreate{print: block.Height}
		}
	})

	err = loader.LoadChain(chain, maxHeight)
	if err != nil {
		logrus.Fatalf("%+v", err)
	}

	close(actions)
	wg.Wait()

	//balances := getBalances()
	//printBalances(balances)
	//balancesToCSV(balances, "balances.csv")
	logrus.Printf("done")
}

type balance struct {
	address string
	amount  uint64
}

type outpoint struct {
	txid string
	nout uint32
}

func (o outpoint) String() string {
	return fmt.Sprintf("%s:%d", o.txid, o.nout)
}

type utxo struct {
	outpoint
	balance
}

type spendOrCreate struct {
	spend  *outpoint
	create *utxo
	print  int // HACK
}

func accountant(actions chan spendOrCreate) {
	for action := range actions {
		if action.print > 0 {
			logrus.Printf("saving balances at height %d", action.print)
			balancesToCSV(getBalances(), fmt.Sprintf("balances_%d.csv", action.print))
		} else if action.create != nil {
			if predeleted[action.create.outpoint.String()] {
				logrus.Debugf("%s was predeleted", action.create.outpoint.String())
				delete(predeleted, action.create.outpoint.String())
			} else {
				utxos[action.create.outpoint.String()] = action.create
			}
		} else {
			if _, ok := utxos[action.spend.String()]; ok {
				delete(utxos, action.spend.String())
			} else {
				predeleted[action.spend.String()] = true
				logrus.Debugf("predeleting %s", action.spend.String())
				////spew.Dump(utxos)
				//panic("deleting nonexistent utxo " + action.spend.String()) // prolly just need to wait for another thread to create utxo
			}
		}
	}
	//spew.Dump(predeleted)
}

func getBalances() map[string]uint64 {
	balances := map[string]uint64{}
	for _, utxo := range utxos {
		balances[utxo.address] += utxo.amount
	}
	return balances
}

func printBalances(balances map[string]uint64) {
	balances["unknown                           "] = balances[""]
	delete(balances, "")

	for address, amount := range balances {
		if amount > 0 {
			logrus.Printf("%s %18d", address, amount)
		}
	}
}

func balancesToCSV(balances map[string]uint64, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	balances["unknown"] = balances[""]
	delete(balances, "")

	writer := csv.NewWriter(f)
	for address, amount := range balances {
		err = writer.Write([]string{address, fmt.Sprintf("%d", amount)})
		if err != nil {
			return err
		}
	}

	return nil
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
