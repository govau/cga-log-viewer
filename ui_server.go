package main

//go:generate go-bindata -o static.go data/

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	cfclient "github.com/cloudfoundry-community/go-cfclient"
	"github.com/govau/cf-common/uaa"
)

type location string

func (l location) BaseURL() string {
	return fmt.Sprintf("https://%scld-logs.kapps.l.cld.gov.au", l[0:1])
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

	// CFEnv must filter
	CFEnv string

	// UAAURL
	UAAUrl string

	ESEndpoint string

	uc      map[string]*userCache
	ucMutex sync.Mutex
}

type userInfo struct {
	GUID string `json:"user_id"`
}

type userCache struct {
	Expires time.Time
	Filter  string
}

func (server *server) augmentRequest(req *http.Request, liu *uaa.LoggedInUser) error {
	server.ucMutex.Lock()
	defer server.ucMutex.Unlock()

	if server.uc == nil {
		server.uc = make(map[string]*userCache)
	}
	val, ok := server.uc[liu.EmailAddress]
	if ok {
		if time.Now().After(val.Expires) {
			val = nil
		}
	}

	if val == nil {
		uaaReq, err := http.NewRequest(http.MethodGet, server.UAAUrl+"/userinfo", nil)
		if err != nil {
			return err
		}
		uaaReq.Header.Set("Authorization", "bearer "+liu.AccessToken)
		resp, err := http.DefaultClient.Do(uaaReq)
		if err != nil {
			return err
		}
		var userInfo userInfo
		err = json.NewDecoder(resp.Body).Decode(&userInfo)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return errors.New("bad status code from userinfo")
		}

		cli, err := cfclient.NewClient(&cfclient.Config{
			ApiAddress: server.API,
			Token:      liu.AccessToken,
		})
		if err != nil {
			return err
		}

		spaces, err := cli.ListUserSpaces(userInfo.GUID)
		if err != nil {
			return err
		}

		var guids []string
		for _, sp := range spaces {
			guids = append(guids, sp.Guid)
		}

		bb, err := json.Marshal(map[string]interface{}{
			"terms_set": map[string]interface{}{
				"@cf.space_id.keyword": map[string]interface{}{
					"terms": guids,
				},
			},
		})
		if err != nil {
			return err
		}

		val = &userCache{
			Filter:  base64.StdEncoding.EncodeToString(bb),
			Expires: time.Now().Add(time.Minute * 5),
		}
		server.uc[liu.EmailAddress] = val
	}

	req.Header.Set("X-ElasticSearch-Filters", val.Filter)
	return nil
}

// Create a handler
func (server *server) CreateHTTPHandler() http.Handler {
	// let's assume kibana does it's own XSRF - TODO fixme
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		liu, ok := r.Context().Value(uaa.KeyLoggedInUser).(*uaa.LoggedInUser)
		if !ok {
			log.Println("bad type")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		url, err := url.Parse(server.ESEndpoint)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		url.Path = r.URL.Path
		url.RawQuery = r.URL.RawQuery
		url.Fragment = r.URL.Fragment

		req, err := http.NewRequest(r.Method, url.String(), r.Body)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err = server.augmentRequest(req, liu)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer resp.Body.Close()

		// Write back headers to requesting client
		rh := w.Header()
		for k, vals := range resp.Header {
			for _, v := range vals {
				rh.Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)

		// Send response back to requesting client
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			log.Println(err) // can't do http.Error as status code is already written
		}
	})
}
