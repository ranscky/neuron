package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileStore implements the Store interface using the local filesystem.
//
// Layout (post-Phase 1):
//
//	data/orgs/<org_id>/
//	  index.json                      — per-org search index
//	  packages/<name>/<version>.tar.gz
//	  packages/<name>/<version>.json
//
// Legacy layout (pre-Phase 1, migrated on first boot):
//
//	data/packages/<name>/<version>.tar.gz
//
// On first NewFileStore() call, if data/packages/ is present, it gets moved
// into data/orgs/public/. Public org is created if needed.
type FileStore struct {
	root string // data dir
}

// NewFileStore creates a new FileStore rooted at the given data dir.
// Run legacy migration (if needed) via MigrateLegacyPackages after construction.
func NewFileStore(root string) (*FileStore, error) {
	fs := &FileStore{root: root}
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("create data root: %w", err)
	}
	return fs, nil
}

// Root returns the data root.
func (fs *FileStore) Root() string {
	return fs.root
}

// Save stores tarballs and manifests under the given org.
func (fs *FileStore) Save(orgID, name, version string, manifest []byte, tarball []byte) error {
	if orgID == "" {
		return fmt.Errorf("orgID is required")
	}

	dir := filepath.Join(fs.root, "orgs", orgID, "packages", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create package dir: %w", err)
	}

	tarballPath := filepath.Join(dir, version+".tar.gz")
	if err := os.WriteFile(tarballPath, tarball, 0644); err != nil {
		return fmt.Errorf("write tarball: %w", err)
	}

	manifestPath := filepath.Join(dir, version+".json")
	if err := os.WriteFile(manifestPath, manifest, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Parse manifest, set orgID, update index
	var pkgInfo PackageInfo
	if err := json.Unmarshal(manifest, &pkgInfo); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	pkgInfo.OrgID = orgID

	idx, err := fs.loadOrgIndex(orgID)
	if err != nil {
		return fmt.Errorf("load org index: %w", err)
	}
	if err := idx.AddPackage(pkgInfo); err != nil {
		return fmt.Errorf("add to index: %w", err)
	}
	data, err := idx.Save()
	if err != nil {
		return fmt.Errorf("serialize index: %w", err)
	}
	if err := os.WriteFile(fs.orgIndexPath(orgID), data, 0644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	return nil
}

// GetManifest retrieves a manifest from the given org.
func (fs *FileStore) GetManifest(orgID, name, version string) ([]byte, error) {
	if orgID == "" {
		return nil, fmt.Errorf("orgID is required")
	}
	path := filepath.Join(fs.root, "orgs", orgID, "packages", name, version+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return data, nil
}

// GetTarball retrieves a tarball from the given org.
func (fs *FileStore) GetTarball(orgID, name, version string) ([]byte, error) {
	if orgID == "" {
		return nil, fmt.Errorf("orgID is required")
	}
	path := filepath.Join(fs.root, "orgs", orgID, "packages", name, version+".tar.gz")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tarball: %w", err)
	}
	return data, nil
}

// ListVersions lists all versions of a package within an org.
func (fs *FileStore) ListVersions(orgID, name string) ([]string, error) {
	if orgID == "" {
		return nil, fmt.Errorf("orgID is required")
	}
	dir := filepath.Join(fs.root, "orgs", orgID, "packages", name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read dir: %w", err)
	}

	versions := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			versions = append(versions, strings.TrimSuffix(entry.Name(), ".json"))
		}
	}
	return versions, nil
}

// Search returns matching packages from a single org.
// Empty orgID searches across all orgs (admin/debug use).
func (fs *FileStore) Search(orgID, query string) ([]PackageInfo, error) {
	if orgID == "" {
		// Cross-org search — fan out across all org dirs.
		return fs.searchAllOrgs(query)
	}
	idx, err := fs.loadOrgIndex(orgID)
	if err != nil {
		return nil, err
	}
	return idx.Search(query), nil
}

// GetLatest returns the latest semver version for a package within an org.
func (fs *FileStore) GetLatest(orgID, name string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("orgID is required for GetLatest")
	}
	idx, err := fs.loadOrgIndex(orgID)
	if err != nil {
		return "", err
	}
	return idx.GetLatest(name)
}

// ---------------------------------------------------------------------------
// Per-org index management
// ---------------------------------------------------------------------------

func (fs *FileStore) orgIndexPath(orgID string) string {
	return filepath.Join(fs.root, "orgs", orgID, "index.json")
}

func (fs *FileStore) loadOrgIndex(orgID string) (*Index, error) {
	idx := NewIndex()
	path := fs.orgIndexPath(orgID)
	if err := idx.LoadOrMigrate(path); err != nil {
		return nil, fmt.Errorf("load org index: %w", err)
	}
	return idx, nil
}

// searchAllOrgs is the cross-org search — only for admin/debug.
// In normal operation, every handler passes a specific orgID from auth context.
func (fs *FileStore) searchAllOrgs(query string) ([]PackageInfo, error) {
	orgsDir := filepath.Join(fs.root, "orgs")
	entries, err := os.ReadDir(orgsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []PackageInfo{}, nil
		}
		return nil, err
	}

	all := []PackageInfo{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		idx, err := fs.loadOrgIndex(entry.Name())
		if err != nil {
			continue // skip broken orgs
		}
		all = append(all, idx.Search(query)...)
	}
	return all, nil
}