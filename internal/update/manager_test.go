package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemanticVersionOrder(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
	}{
		{"v2.0.0-alpha.1", "2.0.0-alpha.2", -1},
		{"2.0.0-alpha.2", "2.0.0-alpha.10", -1},
		{"2.0.0-alpha.10", "2.0.0-beta", -1},
		{"2.0.0-rc.1", "2.0.0", -1},
		{"2.0.0+build.1", "2.0.0+build.2", 0},
		{"2.1.0", "2.0.99", 1},
	} {
		left, leftOK := parseVersion(test.left)
		right, rightOK := parseVersion(test.right)
		if !leftOK || !rightOK {
			t.Fatalf("valid versions rejected: %q %q", test.left, test.right)
		}
		if got := compareVersion(left, right); got != test.want {
			t.Errorf("compareVersion(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
	for _, invalid := range []string{"dev", "v1.2", "1.02.3", "1.2.3-alpha.01", "1.2.3-alpha..1"} {
		if _, ok := parseVersion(invalid); ok {
			t.Errorf("invalid version accepted: %q", invalid)
		}
	}
}

func TestSelectNewestReleaseWithAssets(t *testing.T) {
	releases := `[
			{"tag_name":"v2.0.0-alpha.2","html_url":"https://github.com/Cybervaldemar/sidecast/releases/tag/v2.0.0-alpha.2","assets":[]},
			{"tag_name":"v2.0.0-alpha.3","draft":true},
			{"tag_name":"v2.0.0-alpha.10","html_url":"https://github.com/Cybervaldemar/sidecast/releases/tag/v2.0.0-alpha.10","assets":[
				{"name":"sidecast-windows-amd64.exe","browser_download_url":"https://github.com/Cybervaldemar/sidecast/releases/download/v2.0.0-alpha.10/sidecast-windows-amd64.exe"},
				{"name":"SHA256SUMS","browser_download_url":"https://github.com/Cybervaldemar/sidecast/releases/download/v2.0.0-alpha.10/SHA256SUMS"}]},
			{"tag_name":"not-a-version"}
		]`
	manager := New(func() {})
	manager.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(releases))}, nil
	})}
	selected, err := manager.fetchLatest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if selected.version != "2.0.0-alpha.10" || selected.binary == "" || selected.sums == "" {
		t.Fatalf("wrong release selected: %+v", selected)
	}
	if validAssetURL("https://attacker.example/update.exe", "v2.0.0-alpha.10", assetName) {
		t.Fatal("external asset URL accepted")
	}
}

func TestChecksumAndRejectedDownload(t *testing.T) {
	contents := []byte("a verified executable")
	expected := sha256.Sum256(contents)
	line := hex.EncodeToString(expected[:]) + "  windows-amd64/" + assetName + "\n"
	got, err := checksumFor([]byte(line), "windows-amd64/"+assetName)
	if err != nil || got != expected {
		t.Fatalf("checksumFor = %x, %v", got, err)
	}
	if _, err := checksumFor([]byte(line), assetName); err == nil {
		t.Fatal("checksum from a different path accepted")
	}
	if _, err := checksumFor([]byte(line+line), "windows-amd64/"+assetName); err == nil {
		t.Fatal("duplicate checksum accepted")
	}
	file, err := os.CreateTemp(t.TempDir(), "update-*.exe")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	manager := New(func() {})
	manager.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("tampered executable"))}, nil
	})}
	if err := manager.downloadFile(context.Background(), "https://github.com/asset", file, expected); err == nil || !strings.Contains(err.Error(), "контрольная сумма") {
		t.Fatalf("tampered download accepted: %v", err)
	}
}

func TestReleaseCheckIsCachedDuringPolling(t *testing.T) {
	var requests atomic.Int32
	manager := New(func() {})
	manager.canCheck = true
	manager.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]"))}, nil
	})}
	for range 2 {
		if _, err := manager.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	manager.mu.Lock()
	manager.checkedAt = manager.checkedAt.Add(-time.Hour)
	manager.status = "downloading"
	manager.mu.Unlock()
	if _, err := manager.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("polling made %d GitHub requests, want 1", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
