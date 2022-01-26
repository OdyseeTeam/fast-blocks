package blockchain

import (
	"github.com/cockroachdb/errors"
	"github.com/golang/protobuf/proto"
	pb "github.com/lbryio/types/v2/go"
)

const (
	opReturn = 0x6a //OP_RETURN = 106
	purchase = 0x50 //PURCHASE = 80
)

// IsPurchaseScript returns true if the script for the vout contains the OP_RETURN + 'P' byte identifier for a purchase
func IsPurchaseScript(script []byte) bool {
	if len(script) > 2 {
		if script[0] == opReturn && script[2] == purchase {
			_, err := parsePurchaseScript(script)
			return err == nil
		}
	}
	return false
}

// parsePurchaseScript returns the purchase from script bytes or errors if invalid
func parsePurchaseScript(script []byte) (*pb.Purchase, error) {
	data, err := parseDataScript(script)
	if err != nil {
		return nil, err
	}
	if data[0] != purchase {
		return nil, errors.New("the first byte must be 'P'(0x50) to be a purchase script")
	}
	purchase := &pb.Purchase{}
	err = proto.Unmarshal(data[1:], purchase)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return purchase, nil
}

func parseDataScript(script []byte) ([]byte, error) {
	// OP_RETURN (bytes) DATA
	if len(script) <= 1 {
		return nil, errors.New("there is no script to parse")
	}
	if script[0] != opReturn {
		return nil, errors.New("the first byte of script must be an OP_RETURN to quality as un-spendable data")
	}
	dataBytesToRead := int(script[1])
	if (len(script) - dataBytesToRead - 2) != 0 {
		return nil, errors.Newf("supposed to have %d bytes to read but the script is %d bytes", dataBytesToRead, len(script))
	}
	return script[2:], nil
}
