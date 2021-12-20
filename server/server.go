package server

import (
	"encoding/json"
	"fast-blocks/storage"
	"github.com/genjidb/genji/document"
	"github.com/genjidb/genji/types"
	"github.com/lbryio/lbry.go/v2/extras/errors"
	"github.com/sirupsen/logrus"
	"net/http"
)

func Start() {
	httpServeMux := http.NewServeMux()
	httpServeMux.Handle("/sql", query())
	go func() {
		err := http.ListenAndServe(":8855", httpServeMux)
		if err != nil {
			logrus.Error(err)
		}
	}()
}

func query() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.FormValue("query")
		res, err := storage.DB.Query(q)
		defer res.Close()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		var results = make([]map[string]interface{}, 0)
		err = res.Iterate(func(d types.Document) error {
			var m map[string]interface{}
			err = document.MapScan(d, &m)
			if err != nil {
				return errors.Err(err)
			}
			results = append(results, m)
			return nil
		})

		if err != nil {

		}

		b, err := json.Marshal(results)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		w.Write(b)

	})
}
