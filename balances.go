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
	"github.com/lbryio/lbcd/chaincfg/chainhash"
	"github.com/lbryio/lbcd/txscript"
	"github.com/lbryio/lbcutil"

	"github.com/sirupsen/logrus"
)

func BalanceSnapshots() {
	maxHeight := 889800 // 0 = load it all

	reportBlocks := map[int]struct{}{
		37000:  {}, // 2016 q3
		103000: {}, // 2016 q4
		150900: {}, // 2016 q1
		200000: {}, // 2017 q2
		249000: {}, // 2017 q3
		298000: {}, // jan 1 2018
		347000: {}, // apr 1 2018
		396000: {},
		445000: {},
		495000: {}, // dec 31 2018
		544000: {}, // apr 1 2019
		593000: {},
		642000: {},
		692000: {}, // jan 1 2020
		741000: {},
		790000: {},
		839500: {}, // 2020 q3
		889500: {}, // jan 1 2021
		//938000:  {},
		//988000:  {},
		//1038000: {},
		//1088000: {}, // jan 1 2022
	}

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
				if stream.IsCoinbaseInput(in.TxRef) {
					logrus.Debugf("        IN coinbase")
					continue
				}
				logrus.Debugf("        IN  %s:%d", in.TxRef, in.Position)
				actions <- spendOrCreate{spend: &outpoint{txid: in.TxRef, nout: int(in.Position)}}
			}
			for n, out := range tx.Outputs {
				if out.Address == nil {
					if out.ScriptClass != txscript.NullDataTy {
						logrus.Errorf("no address for %s:%d", out.TransactionHash, n)
					}
					continue
				}

				logrus.Debugf("        OUT %d -> %s (%d)", n, out.Address.String(), out.Amount)
				// outputs create UTXOs
				actions <- spendOrCreate{create: &utxo{
					outpoint: &outpoint{txid: tx.Hash, nout: n},
					balance:  &balance{address: out.Address, amount: out.Amount},
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

	logrus.Printf("done")
}

type balance struct {
	address lbcutil.Address
	amount  uint64
}

type outpoint struct {
	txid *chainhash.Hash
	nout int
}

func (o outpoint) String() string {
	return fmt.Sprintf("%s:%d", o.txid, o.nout)
}

type utxo struct {
	*outpoint
	*balance
}

type spendOrCreate struct {
	spend  *outpoint
	create *utxo
	print  int // HACK
}

type UTXOMap map[chainhash.Hash][]*utxo

func (m UTXOMap) Has(o *outpoint) bool {
	if m[*o.txid] == nil {
		return false
	}
	return m[*o.txid][o.nout] != nil
}

func (m UTXOMap) Add(u *utxo) {
	if len(m[*u.txid]) <= u.nout {
		tmp := m[*u.txid]
		m[*u.txid] = make([]*utxo, u.nout+1)
		copy(m[*u.txid], tmp)
	}
	m[*u.txid][u.nout] = u
}

func (m UTXOMap) Delete(o *outpoint) {
	if m[*o.txid] == nil {
		return
	}
	if len(m[*o.txid]) >= o.nout {
		m[*o.txid][o.nout] = nil
	}

	for _, oo := range m[*o.txid] {
		if oo != nil {
			return
		}
	}

	delete(m, *o.txid)
}

type PredeleteMap map[chainhash.Hash]map[int]struct{}

func (m PredeleteMap) Has(o *outpoint) bool {
	if m[*o.txid] == nil {
		return false
	}
	_, ok := m[*o.txid][o.nout]
	return ok
}

func (m PredeleteMap) Set(o *outpoint) {
	if m[*o.txid] == nil {
		m[*o.txid] = make(map[int]struct{})
	}
	m[*o.txid][o.nout] = struct{}{}
}

func (m PredeleteMap) Delete(o *outpoint) {
	if m[*o.txid] == nil {
		return
	}
	delete(m[*o.txid], o.nout)
	if len(m[*o.txid]) == 0 {
		delete(m, *o.txid)
	}
}

func accountant(actions chan spendOrCreate) {
	utxos := make(UTXOMap)
	predeleted := make(PredeleteMap)

	for a := range actions {
		if a.print > 0 {
			logrus.Printf("saving balances at height %d", a.print)
			err := balancesToCSV(getBalances(utxos), fmt.Sprintf("balances_%d.csv", a.print))
			if err != nil {
				logrus.Error(err)
			}
		} else if a.create != nil {
			if predeleted.Has(a.create.outpoint) {
				logrus.Debugf("%s was predeleted", a.create.outpoint.String())
				predeleted.Delete(a.create.outpoint)
			} else {
				utxos.Add(a.create)
			}
		} else {
			if utxos.Has(a.spend) {
				utxos.Delete(a.spend)
			} else {
				predeleted.Set(a.spend)
				logrus.Debugf("predeleting %s", a.spend.String())
				////spew.Dump(utxos)
				//panic("deleting nonexistent utxo " + action.spend.String()) // prolly just need to wait for another thread to create utxo
			}
		}
	}
	//spew.Dump(predeleted)
}

func getBalances(utxos UTXOMap) map[string]uint64 {
	balances := make(map[string]uint64)
	for _, nouts := range utxos {
		for _, u := range nouts {
			if u != nil {
				balances[u.address.String()] += u.amount
			}
		}
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
