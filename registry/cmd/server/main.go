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
	// Set data root
	dataRoot := "./data"
	if root := os.Getenv("NEURON_DATA_ROOT"); root != "" {
		dataRoot = root
	}

	// Create auth manager
	authMgr := auth.NewManager(dataRoot)

	// Create a new filestore
	// Create a new filestore
	fs, err := store.NewFileStore(dataRoot)
	if err != nil {
		log.Fatalf("Failed to create filestore: %v", err)
	}

	// Run legacy migration to move pre-Phase 1 packages to 'public' org
	if err := store.MigrateLegacyPackages(dataRoot, authMgr); err != nil {
		log.Printf("Warning: legacy migration failed: %v", err)
	}

	// Create a ServeMux
	mux := http.NewServeMux()

	// Register all routes
	mux.Handle("/v1/publish", handlers.NewPublishHandler(fs, authMgr))
	mux.HandleFunc("/v1/packages/", handlers.NewPackagesHandler(fs, authMgr).ServeHTTP)
	mux.Handle("/v1/search", handlers.NewSearchHandler(fs, authMgr))
	mux.Handle("/v1/auth/orgs", handlers.NewAuthHandler(authMgr))

	// Wrap the mux with middleware
	var handler http.Handler = mux
	handler = middleware.Logger(handler)
	handler = middleware.CORS(handler)
	handler = middleware.Recovery(handler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Neuron Registry listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, handler))
}
