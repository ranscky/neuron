// Command neuron-cloud runs the Neuron sync service: device authorisation,
// end-to-end encrypted blob storage, and subscription state.
//
// It is self-hostable and dependency-free. Configure it with:
//
//	PORT                   listen port (default 8080)
//	NEURON_CLOUD_DATA      path to the data file (default ./data/cloud.json)
//	STRIPE_WEBHOOK_SECRET  signing secret; unset disables billing
//	STRIPE_SECRET_KEY      API key used to create Checkout Sessions; unset disables checkout
//	STRIPE_PRICE_ID        recurring price a subscription buys
//	NEURON_PUBLIC_URL      public origin for post-checkout redirects (default: request origin)
//	NEURON_REQUIRE_PRO     "false" disables the paid-plan gate on sync
//	NEURON_OIDC_ISSUER / NEURON_OIDC_CLIENT_ID / NEURON_OIDC_CLIENT_SECRET
//	                       enable provider-backed device approval
//	NEURON_OIDC_REDIRECT_URL overrides the callback URL (default: <public base>/v1/auth/oidc/callback)
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ranscky/neuron-cloud/internal/cloud"
)

func main() {
	logger := log.New(os.Stderr, "neuron-cloud ", log.LstdFlags|log.LUTC)

	dataPath := os.Getenv("NEURON_CLOUD_DATA")
	if dataPath == "" {
		dataPath = "data/cloud.json"
	}

	store, err := cloud.Open(dataPath)
	if err != nil {
		logger.Fatalf("open store: %v", err)
	}

	server := cloud.NewServer(store, os.Getenv("STRIPE_WEBHOOK_SECRET"), logger)
	server.StripeSecretKey = os.Getenv("STRIPE_SECRET_KEY")
	server.StripePriceID = os.Getenv("STRIPE_PRICE_ID")
	server.PublicBaseURL = os.Getenv("NEURON_PUBLIC_URL")
	server.OIDC = cloud.OIDCConfig{
		Issuer:       os.Getenv("NEURON_OIDC_ISSUER"),
		ClientID:     os.Getenv("NEURON_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("NEURON_OIDC_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("NEURON_OIDC_REDIRECT_URL"),
	}
	if os.Getenv("NEURON_REQUIRE_PRO") == "false" {
		server.RequirePro = false
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	logger.Printf("listening on :%s (data %s, billing %v, require-pro %v)",
		port, dataPath, server.WebhookSecret != "", server.RequirePro)

	if err := httpServer.ListenAndServe(); err != nil {
		logger.Fatalf("server stopped: %v", err)
	}
}
