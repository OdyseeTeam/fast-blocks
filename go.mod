module github.com/OdyseeTeam/fast-blocks

go 1.16

replace github.com/btcsuite/btcd => github.com/lbryio/lbrycrd.go v0.0.0-20200203050410-e1076f12bf19

require (
	github.com/btcsuite/btcd v0.0.0-20190213025234-306aecffea32
	github.com/btcsuite/btcutil v0.0.0-20190425235716-9e5f4b9a998d
	github.com/cockroachdb/errors v1.8.6
	github.com/genjidb/genji v0.14.0
	github.com/golang/protobuf v1.5.2
	github.com/lbryio/types v0.0.0-20201019032447-f0b4476ef386
	github.com/sirupsen/logrus v1.8.1
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
)
