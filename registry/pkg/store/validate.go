package store

import "regexp"

// Package names and versions end up in filesystem paths, so they are validated
// before they are ever joined into one. Publishing is org-scoped, which means a
// name is exactly `<org>/<package>`: one slash, no traversal, no absolute paths.
var (
	packageNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*$`)
	versionRE     = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)
)

// ValidPackageName reports whether a name is a safe, org-scoped package name.
func ValidPackageName(name string) bool {
	if len(name) > 128 {
		return false
	}
	// A name is one `<org>/<package>` pair; anything else is either unscoped or
	// trying to escape the data directory.
	if !packageNameRE.MatchString(name) {
		return false
	}
	return !hasDotDot(name)
}

// ValidVersion reports whether a version is safe to use as a file name.
func ValidVersion(version string) bool {
	return versionRE.MatchString(version) && !hasDotDot(version)
}

// hasDotDot catches ".." anywhere, including inside an otherwise valid segment
// such as `acme/..hidden` or `1.0..0`.
func hasDotDot(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '.' && s[i+1] == '.' {
			return true
		}
	}
	return false
}
