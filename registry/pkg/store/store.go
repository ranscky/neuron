package store

import (
	"time"
)

// Store interface
type Store interface {
	Save(name, version string, manifest []byte, tarball []byte) error
	GetManifest(name, version string) ([]byte, error)
	GetTarball(name, version string) ([]byte, error)
	ListVersions(name string) ([]string, error)
	Search(query string) ([]PackageInfo, error)
	GetLatest(name string) (string, error)
}

// PackageInfo struct
type PackageInfo struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Runtime     string    `json:"runtime"`
	Permissions []string  `json:"permissions"`
	PublishedAt time.Time `json:"published_at"`
}