package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"

	"github.com/ranscky/neuron/pkg/manifest"
)

// Package represents a package in the registry
type Package struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// PackageInfo represents detailed information about a package
type PackageInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	// Add other fields as needed
}

// Registry defines the interface for interacting with a package registry
type Registry interface {
	// Search finds packages matching a query
	Search(query string) ([]Package, error)
	
	// Fetch retrieves a package by name and version
	Fetch(name, version string) ([]byte, error)
	
	// GetPackageInfo retrieves detailed information about a package
	GetPackageInfo(name string) (*PackageInfo, error)
	
	// Publish uploads a package to the registry
	Publish(manifest *manifest.Manifest, tarball []byte) error
}

// RegistryClient implements the Registry interface
type RegistryClient struct {
	baseURL string
}

// NewRegistryClient creates a new registry client
func NewRegistryClient(baseURL string) *RegistryClient {
	// If baseURL is not provided, use environment variable or default
	if baseURL == "" {
		baseURL = os.Getenv("NEURON_REGISTRY_URL")
		if baseURL == "" {
			baseURL = "https://neuron-production-ae02.up.railway.app"
		}
	}
	return &RegistryClient{
		baseURL: baseURL,
	}
}

// Search implements Registry.Search
func (r *RegistryClient) Search(query string) ([]Package, error) {
	// Construct the URL
	u, err := url.Parse(r.baseURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %v", err)
	}
	
	u.Path = "/v1/search"
	q := u.Query()
	q.Set("q", query)
	u.RawQuery = q.Encode()
	
	// Make the HTTP request
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("error making search request: %v", err)
	}
	defer resp.Body.Close()
	
	// Check for non-200 response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	// Parse the response
	var result struct {
		Results []Package `json:"results"`
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("error parsing response: %v", err)
	}
	
	return result.Results, nil
}

// Fetch implements Registry.Fetch
func (r *RegistryClient) Fetch(name, version string) ([]byte, error) {
	// Construct the URL
	u, err := url.Parse(r.baseURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %v", err)
	}
	
	u.Path = fmt.Sprintf("/v1/packages/%s/%s/download", name, version)
	
	// Make the HTTP request
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("error making fetch request: %v", err)
	}
	defer resp.Body.Close()
	
	// Check for non-200 response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	// Read the response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	
	return data, nil
}

// Publish implements Registry.Publish
func (r *RegistryClient) Publish(manifest *manifest.Manifest, tarball []byte) error {
	// Construct the URL
	u, err := url.Parse(r.baseURL)
	if err != nil {
		return fmt.Errorf("error parsing base URL: %v", err)
	}
	
	u.Path = "/v1/publish"
	
	// Create a buffer to write our multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	
	// Add manifest field as JSON string
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("error marshaling manifest: %v", err)
	}
	
	if err := writer.WriteField("manifest", string(manifestBytes)); err != nil {
		return fmt.Errorf("error writing manifest field: %v", err)
	}
	
	// Add tarball field
	part, err := writer.CreateFormFile("tarball", "package.tar.gz")
	if err != nil {
		return fmt.Errorf("error creating tarball field: %v", err)
	}
	
	if _, err := part.Write(tarball); err != nil {
		return fmt.Errorf("error writing tarball: %v", err)
	}
	
	// Close the writer to finalize the multipart form
	if err := writer.Close(); err != nil {
		return fmt.Errorf("error closing multipart writer: %v", err)
	}
	
	// Make the HTTP request
	req, err := http.NewRequest("POST", u.String(), &buf)
	if err != nil {
		return fmt.Errorf("error creating request: %v", err)
	}
	
	req.Header.Set("Content-Type", writer.FormDataContentType())
	
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error making publish request: %v", err)
	}
	defer resp.Body.Close()
	
	// Check for non-200 response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("publish failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	return nil
}

// GetPackageInfo fetches detailed information about a package
func (r *RegistryClient) GetPackageInfo(name string) (*PackageInfo, error) {
	// Construct the URL
	u, err := url.Parse(r.baseURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing base URL: %v", err)
	}
	
	u.Path = fmt.Sprintf("/v1/packages/%s", name)
	
	// Make the HTTP request
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("error making request: %v", err)
	}
	defer resp.Body.Close()
	
	// Check for non-200 response
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}
	
	// Parse the response
	var pkg PackageInfo
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %v", err)
	}
	
	if err := json.Unmarshal(body, &pkg); err != nil {
		return nil, fmt.Errorf("error parsing response: %v", err)
	}
	
	return &pkg, nil
}
