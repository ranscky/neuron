package store

import (
	"time"
)

// Store is the package storage interface.
//
// Every method takes an orgID as the first argument. This scopes all reads/writes
// to a specific organization. The store is responsible for ensuring that orgA
// can never see, modify, or enumerate orgB's packages.
//
// Callers (handlers) extract orgID from the authenticated request context.
type Store interface {
	// Save writes a package's tarball and manifest under the given org.
	Save(orgID, name, version string, manifest []byte, tarball []byte) error

	// GetManifest retrieves a manifest from the given org.
	GetManifest(orgID, name, version string) ([]byte, error)

	// GetTarball retrieves a tarball from the given org.
	GetTarball(orgID, name, version string) ([]byte, error)

	// ListVersions lists all versions of a package within the given org.
	ListVersions(orgID, name string) ([]string, error)

	// Search searches within a single org. Empty orgID = all orgs (admin use).
	Search(orgID, query string) ([]PackageInfo, error)

	// GetLatest returns the latest semver version of a package within an org.
	GetLatest(orgID, name string) (string, error)
}

// PackageInfo struct — unchanged. Includes the org for cross-org search results.
type PackageInfo struct {
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Runtime     string    `json:"runtime"`
	Permissions []string  `json:"permissions"`
	PublishedAt time.Time `json:"published_at"`
}