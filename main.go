package main

import (
	"fmt"
	"log"
	"net/http"

	cfenv "github.com/cloudfoundry-community/go-cfenv"
	"github.com/govau/cf-common/env"
	"github.com/govau/cf-common/uaa"

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
		DeniedContent: []byte("denied3243"),
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
		API:         "https://api.system." + envVars.MustString("OUR_LOCATION"),
		Insecure:    envVars.MustBool("INSECURE"),
		OurLocation: location(envVars.MustString("OUR_LOCATION")),
		CSRFKey:     csrfKey,
		CFEnv:       envVars.MustString("OUR_LOCATION"),
		UAAUrl:      uaaURL,
		ESEndpoint:  envVars.MustString("ES_END_POINT"),
	}).CreateHTTPHandler())))
}
