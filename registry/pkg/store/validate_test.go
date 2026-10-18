package store

import "testing"

func TestValidPackageName(t *testing.T) {
	valid := []string{
		"acme/web-search",
		"agents/researcher",
		"tools/web-search",
		"a/b",
		"acme/tool.v2",
		"acme/tool_v2",
		"acme/tool-2",
	}
	for _, name := range valid {
		if !ValidPackageName(name) {
			t.Errorf("%q should be a valid package name", name)
		}
	}

	invalid := []string{
		"",
		"web-search",                        // unscoped
		"acme/",                             // empty package
		"/web-search",                       // empty org
		"acme/web/search",                   // more than one segment
		"Acme/Web-Search",                   // upper case
		"../etc/passwd",                     // traversal without a prefix
		"acme/../../etc/passwd",             // traversal inside the package segment
		"acme/..",                           // traversal as the package
		"acme/..hidden",                     // dot-dot is never allowed
		"acme/web search",                   // space
		"acme/web\\search",                  // backslash
		"C:/acme/web",                       // absolute-ish
		"acme/" + string(make([]byte, 200)), // absurdly long
	}
	for _, name := range invalid {
		if ValidPackageName(name) {
			t.Errorf("%q must be rejected", name)
		}
	}
}

func TestValidVersion(t *testing.T) {
	for _, v := range []string{"1.0.0", "0.1.0", "1.0.0-rc.1", "2.0.0+build.5", "1"} {
		if !ValidVersion(v) {
			t.Errorf("%q should be a valid version", v)
		}
	}
	for _, v := range []string{"", "..", "1.0.0/../x", "1/2", "v1 beta", "a\\b", string(make([]byte, 80))} {
		if ValidVersion(v) {
			t.Errorf("%q must be rejected", v)
		}
	}
}

// The store must not trust its caller: a traversal attempt returns an error and
// writes nothing outside the data directory.
func TestSaveRejectsUnsafeNames(t *testing.T) {
	inTempDataDir(t)

	fs, err := NewFileStore()
	if err != nil {
		t.Fatal(err)
	}

	manifest := []byte(`{"name":"acme/tool","version":"1.0.0"}`)
	cases := [][2]string{
		{"../escape", "1.0.0"},
		{"acme/../../escape", "1.0.0"},
		{"acme/tool", "../1.0.0"},
		{"acme/tool", "1.0.0/../../x"},
		{"unscoped", "1.0.0"},
	}
	for _, tc := range cases {
		if err := fs.Save(tc[0], tc[1], manifest, []byte("bytes")); err == nil {
			t.Errorf("Save(%q, %q) should be refused", tc[0], tc[1])
		}
	}
}
