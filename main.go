package main

//go:generate go-bindata -o static.go data/

import (
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go/aws/credentials"
	cfenv "github.com/cloudfoundry-community/go-cfenv"
	"github.com/govau/cf-common/env"
	"github.com/govau/cf-common/uaa"
	"github.com/olivere/elastic"
	aws "github.com/olivere/elastic/aws/v4"

	"encoding/hex"
)

// Start the app
func main() {
	lookupPath := []env.VarSetOpt{env.WithOSLookup()}
	app, err := cfenv.Current()
	if err == nil {
		lookupPath = append(lookupPath, env.WithUPSLookup(app, "log-viewer-ups"))
	}
	envVars := env.NewVarSet(lookupPath...)

	csrfKey, err := hex.DecodeString(envVars.MustString("CSRF_KEY"))
	if err != nil {
		log.Fatal(err)
	}
	if len(csrfKey) != 32 {
		log.Fatal("CSRF_KEY should be 32 hex-encoded bytes")
	}

	oauthBase := location(envVars.MustString("OUR_LOCATION")).BaseURL()
	if envVars.MustBool("INSECURE") {
		oauthBase = fmt.Sprintf("http://localhost:%s", envVars.MustString("PORT"))
	}

	dd, err := Asset("data/denied.html")
	if err != nil {
		log.Fatal(err)
	}

	elasticClient, err := elastic.NewClient(
		elastic.SetURL(envVars.MustString("AWS_ES_HTTPS_ENDPOINT")),
		elastic.SetSniff(false),
		elastic.SetHealthcheck(false),
		elastic.SetHttpClient(aws.NewV4SigningClient(credentials.NewStaticCredentials(
			envVars.MustString("AWS_ACCESS_KEY_ID"),
			envVars.MustString("AWS_SECRET_ACCESS_KEY"),
			"",
		), "ap-southeast-2")),
	)
	if err != nil {
		log.Fatal(err)
	}

	uaaURL := "https://uaa.system." + envVars.MustString("OUR_LOCATION")
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", envVars.MustString("PORT")), (&uaa.LoginHandler{
		Cookies: uaa.MustCreateBasicCookieHandler(envVars.MustBool("INSECURE")),
		UAA: &uaa.Client{
			URL:          uaaURL,
			ClientID:     envVars.MustString("CLIENT_ID"),
			ClientSecret: envVars.MustString("CLIENT_SECRET"),
			ExternalURL:  uaaURL,
		},
		ExternalUAAURL: uaaURL,
		Scopes: []string{
			"openid",
			"cloud_controller.read",
		},
		BaseURL:       oauthBase,
		DeniedContent: dd,
		ShouldIgnore: func(r *http.Request) bool {
			if r.URL.Path == "/favicon.ico" {
				return true // no auth here (if we do, we get a race condition)
			}
			return false
		},
		AcceptAPIHeader: func(r *http.Request) bool {
			return false
		},
	}).Wrap((&server{
		API:           "https://api.system." + envVars.MustString("OUR_LOCATION"),
		Insecure:      envVars.MustBool("INSECURE"),
		OurLocation:   location(envVars.MustString("OUR_LOCATION")),
		CSRFKey:       csrfKey,
		ElasticClient: elasticClient,
		CFEnv:         envVars.MustString("OUR_LOCATION"),
	}).CreateHTTPHandler())))
}
