package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/OdyseeTeam/fast-blocks/chain"
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

	//BalanceSnapshots(0)
	//ClaimAddresses()
	//AddressesForInputs()
	//MinerAddresses()
	ChangeFromClaims()

	//TotalAddresses()

	//TimeParse()
}

func Benchmark() {
	defer func(start time.Time) {
		logrus.Printf("reading all the block data and doing nothing else took %s", time.Now().Sub(start))
	}(time.Now())

	c, err := chain.NewReader("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}

	c.Workers = runtime.NumCPU() - 1
	c.OnBlock(func(block chain.Block) {})

	err = c.Load()
	if err != nil {
		logrus.Errorf("%+v", err)
	}
}

func AllAddresses() {
	c, err := chain.NewReader("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	c.Workers = runtime.NumCPU() - 1

	addressChan := make(chan string)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		addressCollector(addressChan, fmt.Sprintf("all_addresses_%d", time.Now().Unix()))
		wg.Done()
	}()

	c.OnBlock(func(block chain.Block) {
		if block.IsStale() {
			return
		}
		for _, tx := range block.Transactions {
			for _, out := range tx.Outputs {
				if out.Address != nil {
					addressChan <- out.Address.EncodeAddress()
				}
			}
		}
	})

	err = c.Load()
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	close(addressChan)
	wg.Wait()

	logrus.Printf("done")
}

func AddressesForInputs() {
	c, err := chain.NewReader("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	c.Workers = runtime.NumCPU() - 1

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
		addressCollector(addressChan, fmt.Sprintf("consumptive_addresses_%d", time.Now().Unix()))
		wg.Done()
	}()

	c.OnBlock(func(block chain.Block) {
		if block.IsStale() {
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

	err = c.Load()
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	close(addressChan)
	wg.Wait()

	logrus.Printf("done")
}

// MinerAddresses are all addresses from coinbase transactions
func MinerAddresses() {
	c, err := chain.NewReader("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	c.Workers = runtime.NumCPU() - 1

	addressChan := make(chan string)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		addressCollector(addressChan, fmt.Sprintf("coinbase_addresses_%d", time.Now().Unix()))
		wg.Done()
	}()

	c.OnBlock(func(block chain.Block) {
		for _, tx := range block.Transactions {
			isCoinbase := false
			for _, in := range tx.Inputs {
				if in.IsCoinbase() {
					isCoinbase = true
				}
			}
			if isCoinbase {
				for _, out := range tx.Outputs {
					if out.Address != nil {
						addressChan <- out.Address.EncodeAddress()
					}
				}
				return
			}
		}
	})

	err = c.Load()
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	close(addressChan)
	wg.Wait()

	logrus.Printf("done")
}

// ChangeFromClaims are addresses from outputs of a claim tx that are not the claim itself
func ChangeFromClaims() {
	c, err := chain.NewReader("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	c.Workers = runtime.NumCPU() - 1

	addressChan := make(chan string)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		addressCollector(addressChan, fmt.Sprintf("change_addresses_%d", time.Now().Unix()))
		wg.Done()
	}()

	c.OnBlock(func(block chain.Block) {
		if block.IsStale() {
			return
		}

		for _, tx := range block.Transactions {
			isClaim := false
			for _, out := range tx.Outputs {
				if out.ClaimScript != nil {
					isClaim = true
					break
				}
			}

			if !isClaim {
				continue
			}

			for _, out := range tx.Outputs {
				if out.ClaimScript != nil && out.Address != nil {
					addressChan <- out.Address.EncodeAddress()
				}
			}
		}
	})

	err = c.Load()
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

	c, err := chain.NewReader("/home/grin/.lbrycrd-17.3.3/blocks/")
	if err != nil {
		logrus.Fatalf("%+v", err)
	}
	c.Workers = runtime.NumCPU() - 1

	addressChan := make(chan string)
	outpointChan := make(chan outpoint)
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		addressCollector(addressChan, fmt.Sprintf("claim_addresses_%d", time.Now().Unix()))
		wg.Done()
	}()
	go func() {
		outpointCollector(outpointChan, fmt.Sprintf("consumptive_input_outpoints_%d", time.Now().Unix()))
		wg.Done()
	}()

	c.OnBlock(func(block chain.Block) {
		if block.IsStale() {
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

	err = c.Load()
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	close(addressChan)
	close(outpointChan)
	wg.Wait()

	logrus.Printf("done")
}

func addressCollector(addrChan chan string, filename string) {
	addresses := make(map[string]struct{})
	for a := range addrChan {
		addresses[a] = struct{}{}
	}

	logrus.Printf("saving to file")

	if len(addresses) > 0 {
		f, err := os.Create(filename)
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

	logrus.Printf("collected %d addresses, saved to %s", len(addresses), filename)
}

func outpointCollector(outpointChan chan outpoint, filename string) {
	var outpoints []outpoint

	for o := range outpointChan {
		outpoints = append(outpoints, o)
	}

	logrus.Printf("saving to file")

	if len(outpoints) > 0 {
		f2, err := os.Create(filename)
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

	logrus.Printf("collected %d outpoints, saved to %s", len(outpoints), filename)
}

func printStaleBlockHashes() {
	blocks, err := chain.GetStaleBlockHashes()
	if err != nil {
		logrus.Errorf("%+v", err)
		return
	}

	for _, b := range blocks {
		fmt.Printf(`"%s": {},`+"\n", b)
	}
}
