package registry

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// parseVersion parses a semantic version string into major, minor, patch components
func parseVersion(version string) (major, minor, patch int, err error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid version format: %s", version)
	}
	
	if major, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid major version: %s", parts[0])
	}
	
	if minor, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid minor version: %s", parts[1])
	}
	
	if patch, err = strconv.Atoi(parts[2]); err != nil {
		return 0, 0, 0, fmt.Errorf("invalid patch version: %s", parts[2])
	}
	
	return major, minor, patch, nil
}

// versionMatches checks if a version satisfies a constraint
func versionMatches(version, constraint string) (bool, error) {
	if constraint == "" || constraint == "*" {
		return true, nil
	}
	
	// Exact version match
	if !strings.HasPrefix(constraint, "^") && !strings.HasPrefix(constraint, "~") {
		return version == constraint, nil
	}
	
	// Parse the version we're checking
	vMajor, vMinor, vPatch, err := parseVersion(version)
	if err != nil {
		return false, err
	}
	
	// Parse the constraint version
	constraintVer := constraint[1:] // Remove ^ or ~ prefix
	cMajor, cMinor, cPatch, err := parseVersion(constraintVer)
	if err != nil {
		return false, err
	}
	
	// Caret (^) means compatible with the specified version
	// Allows changes that do not modify the left-most non-zero digit
	if strings.HasPrefix(constraint, "^") {
		if vMajor != cMajor {
			return false, nil
		}
		if vMajor == 0 {
			// For 0.x.y, only allow changes to patch version
			return vMinor == cMinor && vPatch >= cPatch, nil
		}
		// For 1.x.y and above, allow changes to minor and patch versions
		return vMinor >= cMinor, nil
	}
	
	// Tilde (~) means approximately equivalent to the specified version
	// Allows changes that do not modify the major or minor version
	if strings.HasPrefix(constraint, "~") {
		return vMajor == cMajor && vMinor == cMinor && vPatch >= cPatch, nil
	}
	
	return false, nil
}

// compareVersions compares two semantic versions
// Returns -1 if a < b, 0 if a == b, 1 if a > b
func compareVersions(a, b string) (int, error) {
	aMajor, aMinor, aPatch, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	
	bMajor, bMinor, bPatch, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	
	if aMajor != bMajor {
		if aMajor < bMajor {
			return -1, nil
		}
		return 1, nil
	}
	
	if aMinor != bMinor {
		if aMinor < bMinor {
			return -1, nil
		}
		return 1, nil
	}
	
	if aPatch != bPatch {
		if aPatch < bPatch {
			return -1, nil
		}
		return 1, nil
	}
	
	return 0, nil
}

// ResolveVersion resolves a version constraint against a list of available versions
// Returns the best matching version or an error if no match is found
func ResolveVersion(name, constraint string, available []string) (string, error) {
	if len(available) == 0 {
		return "", fmt.Errorf("no versions available for package %s", name)
	}
	
	// Handle exact version constraint
	if constraint == "" || constraint == "*" {
		// Return the latest version
		sort.Slice(available, func(i, j int) bool {
			result, _ := compareVersions(available[i], available[j])
			return result > 0
		})
		return available[0], nil
	}
	
	// Check for exact match first
	if !strings.HasPrefix(constraint, "^") && !strings.HasPrefix(constraint, "~") {
		for _, version := range available {
			if version == constraint {
				return version, nil
			}
		}
		return "", fmt.Errorf("exact version %s not found for package %s", constraint, name)
	}
	
	// Find matching versions
	var matches []string
	for _, version := range available {
		match, err := versionMatches(version, constraint)
		if err != nil {
			return "", fmt.Errorf("error checking version %s: %v", version, err)
		}
		if match {
			matches = append(matches, version)
		}
	}
	
	if len(matches) == 0 {
		return "", fmt.Errorf("no matching version found for constraint %s for package %s", constraint, name)
	}
	
	// Sort matches and return the highest version
	sort.Slice(matches, func(i, j int) bool {
		result, _ := compareVersions(matches[i], matches[j])
		return result > 0
	})
	
	return matches[0], nil
}