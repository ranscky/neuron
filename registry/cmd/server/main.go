package main

import (
	"log"
	"net/http"
	"os"

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

	// Create a ServeMux
	mux := http.NewServeMux()

	// Register all routes
	mux.Handle("/v1/publish", handlers.NewPublishHandler(store))
	mux.Handle("/v1/packages/", handlers.NewPackagesHandler(store))
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
	log.Printf("Neuron Registry listening on :%s", port)

	// Start the server
	log.Fatal(http.ListenAndServe(":"+port, handler))
}