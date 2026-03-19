package runtime

import (
	"fmt"
	"strings"

	"github.com/yourusername/neuron/pkg/manifest"
)

// Sandbox handles permission enforcement and sandboxing
type Sandbox struct{}

// NewSandbox creates a new Sandbox instance
func NewSandbox() *Sandbox {
	return &Sandbox{}
}

// EnforcePermissions checks if the runtime execution complies with the manifest permissions
func (s *Sandbox) EnforcePermissions(manifest *manifest.Manifest, env map[string]string) error {
	// Check if http permission is required but not granted
	requiresHTTP := false
	for _, perm := range manifest.Permissions {
		if perm == "http" {
			requiresHTTP = true
			break
		}
	}
	
	// If http is not in permissions, return an error
	if !requiresHTTP {
		// Check if any environment variables suggest HTTP usage
		for key := range env {
			if strings.Contains(strings.ToLower(key), "http") || 
			   strings.Contains(strings.ToLower(key), "url") ||
			   strings.Contains(strings.ToLower(key), "endpoint") {
				return fmt.Errorf("http permission not granted but HTTP-related environment variables detected")
			}
		}
	}
	
	// Block any environment variables not declared in permissions
	allowedEnvVars := make(map[string]bool)
	for _, perm := range manifest.Permissions {
		if strings.HasPrefix(perm, "env:") {
			envVarName := strings.TrimPrefix(perm, "env:")
			allowedEnvVars[envVarName] = true
		}
	}
	
	// Check if all provided environment variables are allowed
	for key := range env {
		// Skip checking special environment variables that are always allowed
		// (like PATH, HOME, etc. that might be needed for execution)
		if key == "PATH" || key == "HOME" || key == "USER" || key == "PWD" {
			continue
		}
		
		// If this isn't an allowed environment variable, check if it's permitted
		if !allowedEnvVars[key] {
			// Check if there's a permission for this environment variable
			permissionFound := false
			for _, perm := range manifest.Permissions {
				if perm == fmt.Sprintf("env:%s", key) {
					permissionFound = true
					break
				}
			}
			
			if !permissionFound {
				return fmt.Errorf("environment variable %s not declared in permissions", key)
			}
		}
	}
	
	// If we require HTTP permission, make sure it's explicitly granted
	hasHTTPPermission := false
	for _, perm := range manifest.Permissions {
		if perm == "http" {
			hasHTTPPermission = true
			break
		}
	}
	
	if !hasHTTPPermission {
		// Additional check for HTTP-related activities
		// This is a simplified check - in a real implementation, 
		// you might want to monitor network activity
	}
	
	return nil
}