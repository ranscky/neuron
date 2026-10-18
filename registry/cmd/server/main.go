package main

import (
	"log"
	"net/http"
	"os"

	"github.com/ranscky/neuron-registry/pkg/auth"
	"github.com/ranscky/neuron-registry/pkg/handlers"
	"github.com/ranscky/neuron-registry/pkg/middleware"
	"github.com/ranscky/neuron-registry/pkg/store"
)

func main() {
	// Create a new filestore
	store, err := store.NewFileStore()
	if err != nil {
		log.Fatalf("Failed to create filestore: %v", err)
	}

	// Org creation is gated by an admin token, so an instance that was never
	// configured cannot be claimed by whoever finds it first.
	adminToken := os.Getenv("NEURON_REGISTRY_ADMIN_TOKEN")
	orgHandler := handlers.NewOrgHandler(store, adminToken)

	// Create a ServeMux
	mux := http.NewServeMux()

	// Register all routes. Publishing is the only authenticated surface: reads
	// stay public so the registry remains a public catalogue.
	mux.Handle("/v1/publish", auth.RequireOrg(store, handlers.NewPublishHandler(store)))
	mux.HandleFunc("/v1/orgs", orgHandler.Create)
	mux.Handle("/v1/keys", auth.RequireOrg(store, http.HandlerFunc(orgHandler.CreateKey)))
	mux.HandleFunc("/v1/packages/", handlers.NewPackagesHandler(store).ServeHTTP)
	mux.Handle("/v1/search", handlers.NewSearchHandler(store))

	// Wrap the mux with middleware in the specified order:
	// Recovery → CORS → Logger
	var handler http.Handler = mux
	handler = middleware.Logger(handler)
	handler = middleware.CORS(handler)
	handler = middleware.Recovery(handler)

	// Read port from PORT environment variable, default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Log startup message
	orgCreation := "disabled (set NEURON_REGISTRY_ADMIN_TOKEN to enable)"
	if adminToken != "" {
		orgCreation = "enabled"
	}
	log.Printf("Neuron Registry listening on :%s (org creation %s)", port, orgCreation)

	// Start the server
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
