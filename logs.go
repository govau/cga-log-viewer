package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/govau/cf-common/uaa"
	"github.com/olivere/elastic"
)

type userInfo struct {
	GUID string `json:"user_id"`
}

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
	q := r.FormValue("query")
	from := r.FormValue("from")
	to := r.FormValue("to")
	limit := r.FormValue("limit")
	if q == "" {
		q = "*"
	}
	if from == "" {
		from = "1h"
	}
	if to == "" {
		to = "0s"
	}
	if limit == "" {
		limit = "100"
	}

	var query elastic.Query
	var a cfclient.App
	var err error
	var rs resultSet
	var results *elastic.SearchResult
	var limitInt int
	var fromDuration, toDuration time.Duration
	var src interface{}
	var data []byte
	var spaces []cfclient.Space
	var req *http.Request
	var resp *http.Response
	var userInfo userInfo
	var spaceIDs []interface{}

	now := time.Now()

	// By calling CF as the user, this has the side-effect of verifying
	// that the user has a level of access to the app.
	// TODO: consider verifying a bit more affirmatively
	a, err = cli.AppByGuid(vars["app"])
	if err != nil {
		goto end
	}

	req, err = http.NewRequest(http.MethodGet, server.UAAUrl+"/userinfo", nil)
	if err != nil {
		goto end
	}
	req.Header.Set("Authorization", "bearer "+liu.AccessToken)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		goto end
	}
	err = json.NewDecoder(resp.Body).Decode(&userInfo)
	resp.Body.Close()
	if err != nil {
		goto end
	}
	if resp.StatusCode != http.StatusOK {
		err = errors.New("bad status code from userinfo")
		goto end
	}

	spaces, err = cli.ListUserSpaces(userInfo.GUID)
	if err != nil {
		goto end
	}

	limitInt, err = strconv.Atoi(limit)
	if err != nil {
		goto end
	}

	fromDuration, err = time.ParseDuration(from)
	if err != nil {
		goto end
	}
	toDuration, err = time.ParseDuration(to)
	if err != nil {
		goto end
	}

	spaceIDs = make([]interface{}, len(spaces))
	for idx, sp := range spaces {
		spaceIDs[idx] = sp.Guid
	}

	query = elastic.NewBoolQuery().Filter(
		elastic.NewRangeQuery("kinesis_time").Gte(now.Add(-fromDuration).UnixNano()/1000000).Lt(now.Add(-toDuration).UnixNano()/1000000),
		elastic.NewTermQuery("@cf.env.keyword", server.CFEnv),
		elastic.NewTermsSetQuery("@cf.space_id.keyword", spaceIDs...),
		//elastic.NewTermQuery("@cf.app.keyword", stripSuffixes(a.Name)), // we use name here as it's more robust across blue/green style deployments
	).Must(elastic.NewQueryStringQuery(q))

	src, err = query.Source()
	if err != nil {
		goto end
	}

	data, err = json.MarshalIndent(src, "", "  ")
	if err != nil {
		goto end
	}

	results, err = server.ElasticClient.Search("_all").Query(query).Size(limitInt).Do(r.Context())
	if err != nil {
		goto end
	}

	rs.Headers = []string{"Result"}
	if results.Hits == nil {
		err = fmt.Errorf("no hits detected")
		goto end
	}

	for _, sh := range results.Hits.Hits {
		b, err := sh.Source.MarshalJSON()
		if err != nil {
			goto end
		}

		// now unmarshall so we can delete fields - there must be a better way?
		var v map[string]interface{}
		err = json.Unmarshal(b, &v)
		if err != nil {
			goto end
		}
		delete(v, "@cf.app")
		delete(v, "@cf.space")
		delete(v, "@cf.org")
		delete(v, "@cf.app_id")
		delete(v, "@cf.space_id")
		delete(v, "@cf.org_id")
		delete(v, "@cf.env")

		b, err = json.Marshal(v)
		if err != nil {
			goto end
		}

		rs.Rows = append(rs.Rows, []string{string(b)})
	}

end:
	var message string
	if err != nil {
		message = err.Error()
	}
	return map[string]interface{}{
		"app":     a,
		"query":   q,
		"from":    from,
		"to":      to,
		"limit":   limit,
		"esquery": string(data),
		"message": message,
		"results": []resultSet{rs},
	}, nil
}
