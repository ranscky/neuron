// Command neuron-cloud runs the Neuron sync service: device authorisation,
// end-to-end encrypted blob storage, and subscription state.
//
// It is self-hostable and dependency-free. Configure it with:
//
//	PORT                   listen port (default 8080)
//	NEURON_CLOUD_DATA      path to the data file (default ./data/cloud.json)
//	STRIPE_WEBHOOK_SECRET  signing secret; unset disables billing
//	NEURON_REQUIRE_PRO     "false" disables the paid-plan gate on sync
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
