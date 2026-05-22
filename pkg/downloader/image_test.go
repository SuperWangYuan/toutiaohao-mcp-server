package downloader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImageProcessor_LocalFileExists(t *testing.T) {
	tmpDir := t.TempDir()
	imgPath := filepath.Join(tmpDir, "test.jpg")
	os.WriteFile(imgPath, []byte("fake image"), 0644)

	result, err := ValidateImagePath(imgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != imgPath {
		t.Errorf("result = %s, want %s", result, imgPath)
	}
}

func TestImageProcessor_LocalFileNotExists(t *testing.T) {
	_, err := ValidateImagePath("/nonexistent/path/image.jpg")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestImageProcessor_InvalidExtension(t *testing.T) {
	tmpDir := t.TempDir()
	txtPath := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(txtPath, []byte("not an image"), 0644)

	_, err := ValidateImagePath(txtPath)
	if err == nil {
		t.Error("expected error for .txt file")
	}
}

func TestImageProcessor_HTTPURLFormat(t *testing.T) {
	result, err := ValidateImagePath("https://example.com/image.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "https://example.com/image.jpg" {
		t.Errorf("result = %s, want URL unchanged", result)
	}
}

func TestImageProcessor_EmptyPath(t *testing.T) {
	_, err := ValidateImagePath("")
	if err == nil {
		t.Error("expected error for empty path")
	}
}
