package cookies

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCookieSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cookies.json")
	store := NewFileCookieStore(path)

	data := []byte(`[{"name":"sid","value":"abc123"}]`)
	if err := store.SaveCookies(data); err != nil {
		t.Fatalf("SaveCookies failed: %v", err)
	}

	loaded, err := store.LoadCookies()
	if err != nil {
		t.Fatalf("LoadCookies failed: %v", err)
	}
	if string(loaded) != string(data) {
		t.Errorf("LoadCookies = %s, want %s", loaded, data)
	}
}

func TestCookieLoadFileNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "nonexistent.json")
	store := NewFileCookieStore(path)

	data, err := store.LoadCookies()
	if err != nil {
		t.Fatalf("LoadCookies should not error for missing file: %v", err)
	}
	if data != nil {
		t.Errorf("LoadCookies = %s, want nil", data)
	}
}

func TestCookieDeleteFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cookies.json")
	store := NewFileCookieStore(path)

	// Create the file first
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatalf("setup: WriteFile failed: %v", err)
	}

	if err := store.DeleteCookies(); err != nil {
		t.Fatalf("DeleteCookies failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Cookie file should be deleted, but still exists")
	}
}
