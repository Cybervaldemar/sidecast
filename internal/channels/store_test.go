package channels

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPermanentOwnershipSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := store.Claim("cyber", "")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Owns("cyber", secret) || store.Owns("cyber", "wrong") {
		t.Fatal("invalid ownership")
	}
	if _, err = store.Claim("cyber", ""); err != ErrTaken {
		t.Fatal("nickname must stay reserved")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "channels.json"))
	if strings.Contains(string(b), secret) {
		t.Fatal("plaintext secret was persisted")
	}
	restarted, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Exists("cyber") || !restarted.Owns("cyber", secret) {
		t.Fatal("ownership lost on restart")
	}
	if again, err := restarted.Claim("cyber", secret); err != nil || again != secret {
		t.Fatal("recovery failed")
	}
}

func TestConcurrentClaimsHaveOneOwner(t *testing.T) {
	store, _ := Open(t.TempDir())
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := store.Claim("same-name", ""); results <- err }()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if err != ErrTaken {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected one owner, got %d", winners)
	}
}

func TestInvalidNicknamesAndFailedPersistence(t *testing.T) {
	for _, nick := range []string{"ab", "../file", "-name", "Aname", "имя", strings.Repeat("a", 33)} {
		if Valid(nick) {
			t.Fatalf("accepted %q", nick)
		}
	}
	store, _ := Open(t.TempDir())
	store.path = filepath.Join(t.TempDir(), "missing", "channels.json")
	if _, err := store.Claim("sample", ""); err == nil {
		t.Fatal("write should fail")
	}
	if store.Exists("sample") {
		t.Fatal("failed claim reserved nickname")
	}
}
