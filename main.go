package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/OdyseeTeam/fast-blocks/blockchain"
	"github.com/OdyseeTeam/fast-blocks/storage"
	"github.com/lbryio/lbcd/txscript"
	"github.com/lbryio/lbry.go/v3/schema/address/base58"
	"github.com/lbryio/lbry.go/v3/schema/stake"
	"github.com/sirupsen/logrus"
)

const deweysInLBC = 10 ^ 8 // 1 LBC is this many deweys
const blocksPerDay = 537   // 1 every 161 sec

func main() {
	//defer profile.Start(profile.MemProfile).Stop()
	//logrus.SetLevel(logrus.DebugLevel)

	storage.Start()

	//printStaleBlockHashes()

	BalanceSnapshots(0)
	//ClaimAddresses()
	//AddressesForInputs()

	//TotalAddresses()

	//TimeParse()
}

func TimeParse() {
	chain, err := blockchain.New("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	//chain.Workers = runtime.NumCPU() - 1

	chain.OnBlock(func(block blockchain.Block) {})

	err = chain.Go(0)
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	logrus.Printf("done")
}

func TotalAddresses() {
	maxHeight := 0 // 0 = load it all

	chain, err := blockchain.New("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	chain.Workers = runtime.NumCPU() - 1

	addressChan := make(chan string)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		addressCollector(addressChan, nil)
		wg.Done()
	}()

	chain.OnBlock(func(block blockchain.Block) {
		if _, ok := blockchain.StaleBlockHashes[block.Header.BlockHash.String()]; ok {
			return
		}

		logrus.Debugf("BLOCK %d (%s)", block.Height, block.Header.BlockHash)
		for _, tx := range block.Transactions {
			logrus.Debugf("    TX %s", tx.Hash)
			for n, out := range tx.Outputs {
				if out.Address != nil {
					addressChan <- out.Address.EncodeAddress()
				}
				logrus.Debugf("        OUT %d -> %s (%d)", n, out.Address, out.Amount)
			}
		}
	})

	err = chain.Go(maxHeight)
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	close(addressChan)
	wg.Wait()

	logrus.Printf("done")
}

func AddressesForInputs() {
	maxHeight := 0 // 0 = load it all

	chain, err := blockchain.New("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	chain.Workers = runtime.NumCPU() - 1

	logrus.Infof("loading outpoints")
	outpointList := map[string]struct{}{}
	f, err := os.Open("consumptive_outpoints")
	if err != nil {
		logrus.Fatal(err)
	}

	fileScanner := bufio.NewScanner(f)
	fileScanner.Split(bufio.ScanLines)
	for fileScanner.Scan() {
		outpointList[fileScanner.Text()] = struct{}{}
	}
	f.Close()
	logrus.Infof("done loading outpoints")

	addressChan := make(chan string)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		addressCollector(addressChan, nil)
		wg.Done()
	}()

	chain.OnBlock(func(block blockchain.Block) {
		if _, ok := blockchain.StaleBlockHashes[block.Header.BlockHash.String()]; ok {
			return
		}

		logrus.Debugf("BLOCK %d (%s)", block.Height, block.Header.BlockHash)
		for _, tx := range block.Transactions {
			logrus.Debugf("    TX %s", tx.Hash)
			for n, out := range tx.Outputs {
				if _, ok := outpointList[fmt.Sprintf("%s:%d", tx.Hash, n)]; ok {
					addressChan <- out.Address.EncodeAddress()
				}
				logrus.Debugf("        OUT %d -> %s (%d)", n, out.Address, out.Amount)
			}
		}
	})

	err = chain.Go(maxHeight)
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	close(addressChan)
	wg.Wait()

	logrus.Printf("done")
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
	chain.Workers = runtime.NumCPU() - 1

	addressChan := make(chan string)
	outpointChan := make(chan outpoint)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		addressCollector(addressChan, outpointChan)
		wg.Done()
	}()

	chain.OnBlock(func(block blockchain.Block) {
		if _, ok := blockchain.StaleBlockHashes[block.Header.BlockHash.String()]; ok {
			return
		}

		logrus.Debugf("BLOCK %d (%s)", block.Height, block.Header.BlockHash)
		for _, tx := range block.Transactions {
			logrus.Debugf("    TX %s", tx.Hash)
			hasConsumptiveUse := false
			for n, out := range tx.Outputs {
				if out.ClaimScript != nil {
					hasConsumptiveUse = true
					addressChan <- out.Address.EncodeAddress()

					switch out.ClaimScript.Opcode {
					case txscript.OP_CLAIMNAME, txscript.OP_UPDATECLAIM:
						h, err := stake.DecodeClaimBytes(out.ClaimScript.Value, "")
						if err != nil {
							// don't worry about claims you can't decode. there are a bunch early in the chain
							//logrus.Errorf("could not unmarshall value in %s:%d", tx.Hash, n)
							//logrus.Errorln(hex.EncodeToString(out.ClaimScript.Value))
						} else if !h.IsClaim() {
							// don't worry about claims you can't decode. there are a bunch early in the chain
							//logrus.Errorf("unmarshall fine but its not a claim in %s:%d", tx.Hash, n)
							//logrus.Errorln(hex.EncodeToString(out.ClaimScript.Value))
						} else if s := h.Claim.GetStream(); s != nil && s.GetFee().GetAddress() != nil {
							addressChan <- base58.EncodeBase58(s.GetFee().GetAddress())
						}
					case txscript.OP_SUPPORTCLAIM:
					default:
						logrus.Errorf("unknown opcode in %s:%d", tx.Hash, n)
					}
				} else if out.Purchase != nil {
					hasConsumptiveUse = true
				}

				logrus.Debugf("        OUT %d -> %s (%d)", n, out.Address, out.Amount)
			}

			if !hasConsumptiveUse {
				continue
			}

			//if tx has a claim, then inputs are legit user addresses
			for _, in := range tx.Inputs {
				outpointChan <- outpoint{in.PrevTxHash, int(in.PrevTxIndex)}
			}
		}
	})

	err = chain.Go(maxHeight)
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	close(addressChan)
	close(outpointChan)
	wg.Wait()

	logrus.Printf("done")
}

func addressCollector(addrChan chan string, outpointChan chan outpoint) {
	addresses := make(map[string]struct{})
	var outpoints []outpoint

	toClose := 0
	if addrChan != nil {
		toClose++
	}
	if outpointChan != nil {
		toClose++
	}

	for {
		if toClose <= 0 {
			break
		}

		select {
		case a, ok := <-addrChan:
			if !ok {
				toClose--
				addrChan = nil
				continue
			}
			addresses[a] = struct{}{}
		case o, ok := <-outpointChan:
			if !ok {
				toClose--
				outpointChan = nil
				continue
			}
			outpoints = append(outpoints, o)
		}
	}

	logrus.Printf("saving to file")

	if len(addresses) > 0 {
		f, err := os.Create(fmt.Sprintf("addresses_%d", time.Now().Unix()))
		if err != nil {
			logrus.Error(err)
			return
		}
		defer f.Close()

		for a := range addresses {
			_, err := f.WriteString(a + "\n")
			if err != nil {
				logrus.Fatal(err)
			}
		}
	}

	if len(outpoints) > 0 {
		f2, err := os.Create(fmt.Sprintf("outpoints_%d", time.Now().Unix()))
		if err != nil {
			logrus.Error(err)
			return
		}
		defer f2.Close()

		for _, o := range outpoints {
			_, err := f2.WriteString(o.String() + "\n")
			if err != nil {
				logrus.Fatal(err)
			}
		}
	}

	logrus.Printf("collected %d addresses and %d outpoints", len(addresses), len(outpoints))
}

func printStaleBlockHashes() {
	blocks, err := blockchain.GetStaleBlockHashes()
	if err != nil {
		logrus.Errorf("%+v", err)
		return
	}

	for _, b := range blocks {
		fmt.Printf(`"%s": {},`+"\n", b)
	}
}
