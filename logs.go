package main

import (
	"net/http"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/govau/cf-common/uaa"
	"github.com/olivere/elastic"
)

type resultSet struct {
	Headers []string
	Rows    [][]string
}

// search logs for an app
func (server *server) logs(cli *cfclient.Client, vars map[string]string, liu *uaa.LoggedInUser, w http.ResponseWriter, r *http.Request) (map[string]interface{}, error) {
	a, err := cli.AppByGuid(vars["app"])
	if err != nil {
		return nil, err
	}

	results, err := server.ElasticClient.Search("_all").Query(elastic.NewQueryStringQuery(vars["query"])).Size(100).Do(r.Context())
	var message string
	var rs resultSet

	if err != nil {
		message = err.Error()
	} else {
		rs.Headers = []string{"Result"}
		if results.Hits != nil {
			for _, sh := range results.Hits.Hits {
				b, err := sh.Source.MarshalJSON()
				if err != nil {
					return nil, err
				}
				rs.Rows = append(rs.Rows, []string{string(b)})
			}
		}
	}

	return map[string]interface{}{
		"app":     a,
		"message": message,
		"results": rs,
	}, nil
}
