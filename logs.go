package main

import (
	"fmt"
	"net/http"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/govau/cf-common/uaa"
)

// search logs for an app
func (server *server) logs(cli *cfclient.Client, vars map[string]string, liu *uaa.LoggedInUser, w http.ResponseWriter, r *http.Request) (map[string]interface{}, error) {
	a, err := cli.AppByGuid(vars["app"])
	if err != nil {
		return nil, err
	}

	results, err := server.ElasticClient.Search("_all").Size(100).Do(r.Context())
	var message string
	if err != nil {
		message = err.Error()
	} else {
		message = fmt.Sprintf("%#v", results)
	}

	return map[string]interface{}{
		"app":     a,
		"message": message,
	}, nil
}
