package release_test

import (
	"testing"

	"github.com/cieons/koko/internal/shared/release"
)

// A release's koko is koko-<os>-<arch>, with .exe on Windows alone, so no
// name of an earlier release changes (ADR 0026, ADR 0052).
func TestKokoAssetNamesEachPlatformsKoko(t *testing.T) {
	t.Parallel()
	for platform, want := range map[string]string{
		"linux/amd64":   "koko-linux-amd64",
		"linux/arm64":   "koko-linux-arm64",
		"darwin/arm64":  "koko-darwin-arm64",
		"windows/amd64": "koko-windows-amd64.exe",
	} {
		if got := release.KokoAsset(platform); got != want {
			t.Errorf("KokoAsset(%q) = %q, want %q", platform, got, want)
		}
	}
}
