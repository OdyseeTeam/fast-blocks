package storage

import (
	"github.com/genjidb/genji"
	"github.com/sirupsen/logrus"
)

var DB *genji.DB

func Start() {
	var err error
	DB, err = genji.Open(":memory:")
	if err != nil {
		logrus.Fatal(err)
	}
	err = DB.Exec("CREATE TABLE blocks")
	err = DB.Exec("CREATE TABLE transactions")
	err = DB.Exec("CREATE TABLE inputs")
	err = DB.Exec("CREATE TABLE outputs")
	err = DB.Exec("CREATE TABLE claims")
}
