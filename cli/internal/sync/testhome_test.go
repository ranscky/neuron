package sync

import "testing"

// setHome isolates a test's ~/.neuron state to a fresh temporary directory.
//
// Both HOME and USERPROFILE are set on purpose: os.UserHomeDir reads HOME on
// Unix but USERPROFILE on Windows, so setting only HOME leaves every test on
// Windows sharing the real home directory — which makes these tests fail, and
// leak state into each other, instead of exercising the code.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	setHomeTo(t, home)
	return home
}

// setHomeTo points a test at a specific directory.
func setHomeTo(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}
