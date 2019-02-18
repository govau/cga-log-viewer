package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/govau/cf-common/uaa"
	"github.com/olivere/elastic"
)

type resultSet struct {
	Headers []string
	Rows    [][]string
}

func stripSuffixes(s string) string {
	for _, x := range []string{"-venerable", "-blue", "-green"} {
		if strings.HasSuffix(s, x) {
			return s[:len(s)-len(x)]
		}
	}
	return s
}

// search logs for an app
func (server *server) logs(cli *cfclient.Client, vars map[string]string, liu *uaa.LoggedInUser, w http.ResponseWriter, r *http.Request) (map[string]interface{}, error) {
	// By calling CF as the user, this has the side-effect of verifying
	// that the user has a level of access to the app.
	// TODO: consider verifying a bit more affirmatively
	a, err := cli.AppByGuid(vars["app"])
	if err != nil {
		return nil, err
	}

	q := r.FormValue("query")
	query := elastic.NewBoolQuery().Filter(
		elastic.NewTermQuery("@cf.env", server.CFEnv),
		elastic.NewTermQuery("@cf.space_id", a.SpaceGuid),
		elastic.NewTermQuery("@cf.app", stripSuffixes(a.Name)), // we use name here as it's more robust across blue/green style deployments
	).Must(elastic.NewMatchAllQuery())

	src, err := query.Source()
	if err != nil {
		log.Println(err)
	}
	data, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		log.Println(err)
	}
	log.Printf("Query:\n%s", data)

	results, err := server.ElasticClient.Search("_all").Query(query).Size(100).Do(r.Context())
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
		"query":   q,
		"message": message,
		"results": []resultSet{rs},
	}, nil
}
