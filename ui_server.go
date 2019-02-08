package main

//go:generate go-bindata -o static.go data/

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/gorilla/csrf"
	"github.com/gorilla/mux"
	"github.com/govau/cf-common/uaa"
	"github.com/olivere/elastic"
)

type location string

func (l location) BaseURL() string {
	return fmt.Sprintf("https://db-export.system.%s", l)
}

func newLocationList(s string) []location {
	var rv []location
	for _, l := range strings.Split(s, ",") {
		rv = append(rv, location(l))
	}
	return rv
}

// server contains the config needed for a running server
type server struct {
	// Base URL for the CF API server
	API string

	// If set, disable some CSRF and secure-cookie stuff.
	Insecure bool

	// Base URL of our server
	OurLocation location

	// Would rather make this random each time, but this breaks browsers until the cookies expire. Should be 32 bytes.
	CSRFKey []byte

	// Client for searching logs
	ElasticClient *elastic.Client
}

type resultSet struct {
	Headers []string
	Rows    [][]string
}

// Shows all apps for space
func (server *server) apps(cli *cfclient.Client, vars map[string]string, liu *uaa.LoggedInUser, w http.ResponseWriter, r *http.Request) (map[string]interface{}, error) {
	apps, err := cli.ListAppsByQuery(url.Values{
		"q": {"space_guid:" + vars["space"]},
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"apps": apps,
	}, nil
}

// Shows all spaces for an organization
func (server *server) spaces(cli *cfclient.Client, vars map[string]string, liu *uaa.LoggedInUser, w http.ResponseWriter, r *http.Request) (map[string]interface{}, error) {
	spaces, err := cli.OrgSpaces(vars["org"])
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"spaces": spaces,
	}, nil

}

// Shows all organizations for a user
func (server *server) orgs(cli *cfclient.Client, vars map[string]string, liu *uaa.LoggedInUser, w http.ResponseWriter, r *http.Request) (map[string]interface{}, error) {
	orgs, err := cli.ListOrgs()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"orgs": orgs,
	}, nil
}

// Shows the homepage for a user
func (server *server) home(cli *cfclient.Client, vars map[string]string, liu *uaa.LoggedInUser, w http.ResponseWriter, r *http.Request) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// Fetch the logged in user, and create a cloudfoundry client object and pass that to the underlying real handler.
// Finally, if a template name is specified, and no error returned, execute the template with the values returned
func (server *server) wrapWithClient(tmpl string, f func(cli *cfclient.Client, vars map[string]string, liu *uaa.LoggedInUser, w http.ResponseWriter, r *http.Request) (map[string]interface{}, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		liu, ok := r.Context().Value(uaa.KeyLoggedInUser).(*uaa.LoggedInUser)
		if !ok {
			log.Println("bad type")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		cli, err := cfclient.NewClient(&cfclient.Config{
			ApiAddress: server.API,
			Token:      liu.AccessToken,
		})
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		toPass, err := f(cli, mux.Vars(r), liu, w, r)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// If no template is desired, then stop here
		if tmpl == "" {
			return
		}

		data, err := Asset("data/" + tmpl)
		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		toPass["user"] = liu
		toPass[csrf.TemplateTag] = csrf.TemplateField(r)
		template.Must(template.New("orgs").Parse(string(data))).Execute(w, toPass)
	}
}

// Create a handler
func (server *server) CreateHTTPHandler() http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/app/{app}/logs", server.wrapWithClient("logs.html", server.logs))
	r.HandleFunc("/space/{space}/apps", server.wrapWithClient("apps.html", server.apps))
	r.HandleFunc("/org/{org}/spaces", server.wrapWithClient("spaces.html", server.spaces))
	r.HandleFunc("/orgs", server.wrapWithClient("orgs.html", server.orgs))
	r.HandleFunc("/", server.wrapWithClient("index.html", server.home))

	// Wrap nearly everything with a CSRF
	var opts []csrf.Option
	if server.Insecure {
		opts = append(opts, csrf.Secure(false))
	}

	return csrf.Protect(server.CSRFKey, opts...)(r)
}
