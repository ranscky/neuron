package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// RegistryClient provides a client for interacting with the Neuron Registry.
type RegistryClient struct {
	BaseURL    string
	Token      string
	OrgID      string
	HTTPClient *http.Client
}

// NewRegistryClient creates a new RegistryClient.
func NewRegistryClient(baseURL, token, orgID string) *RegistryClient {
	return &RegistryClient{
		BaseURL:    baseURL,
		Token:      token,
		OrgID:      orgID,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Publish uploads a package to the registry.
func (c *RegistryClient) Publish(name, version, manifestJSON string, tarballPath string) error {
	if c.Token == "" {
		return fmt.Errorf("registry token is missing; run 'neuron login <token>' first")
	}

	tarballData, err := os.ReadFile(tarballPath)
	if err != nil {
		return fmt.Errorf("read tarball: %w", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if err := writer.WriteField("manifest", manifestJSON); err != nil {
		return fmt.Errorf("write manifest field: %w", err)
	}

	part, err := writer.CreateFormFile("tarball", filepath.Base(tarballPath))
	if err != nil {
		return fmt.Errorf("create tarball part: %w", err)
	}
	if _, err := part.Write(tarballData); err != nil {
		return fmt.Errorf("write tarball data: %w", err)
	}
	writer.Close()

	req, err := http.NewRequest("POST", c.BaseURL+"/v1/publish", body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// Fetch downloads a package tarball.
func (c *RegistryClient) Fetch(name, version string) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/packages/%s/%s/download", c.BaseURL, name, version)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Search searches for packages in a specific org.
func (c *RegistryClient) Search(orgID, query string) ([]PackageInfo, error) {
	url := fmt.Sprintf("%s/v1/search?q=%s", c.BaseURL, query)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var results []PackageInfo
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode search results: %w", err)
	}
	return results, nil
}

// GetLatest returns the latest version of a package in a specific org.
func (c *RegistryClient) GetLatest(orgID, name string) (string, error) {
	url := fmt.Sprintf("%s/v1/packages/%s", c.BaseURL, name)
	req, httpReq := http.NewRequest("GET", url, nil)
	_ = httpReq // ignore
	if err := func() error {
		req.Header.Set("Authorization", "Bearer "+c.Token)
		return nil
	}(); err != nil {
		return "", err
	}
	
	// This is a bit of a hack because I just want the version.
	// The handler returns the full PackageInfo.
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var info PackageInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("decode package info: %w", err)
	}
	return info.Version, nil
}

// PackageInfo matches the registry's PackageInfo struct.
type PackageInfo struct {
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Runtime     string    `json:"runtime"`
	Permissions []string  `json:"permissions"`
	PublishedAt string    `json:"published_at"`
}
