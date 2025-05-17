package main

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDownloadFile(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("mock file content"))
	}))
	defer mockServer.Close()

	outputFile := "testfile.txt"
	err := downloadFile(mockServer.URL, outputFile)
	if err != nil {
		t.Fatalf("Failed to download file: %v", err)
	}
	defer os.Remove(outputFile)

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	expectedContent := "mock file content"
	if string(content) != expectedContent {
		t.Errorf("Expected %s, but got %s", expectedContent, string(content))
	}
}

func TestUnzipGZFile(t *testing.T) {
	content := "This is a test file for unzipping."
	gzFile := "testfile.gz"

	file, err := os.Create(gzFile)
	if err != nil {
		t.Fatalf("Failed to create test gzip file: %v", err)
	}
	defer os.Remove(gzFile)

	writer := gzip.NewWriter(file)
	_, err = writer.Write([]byte(content))
	if err != nil {
		t.Fatalf("Failed to write to gzip file: %v", err)
	}
	writer.Close()
	file.Close()

	data, err := unzipGZFile(gzFile)
	if err != nil {
		t.Fatalf("Failed to unzip file: %v", err)
	}

	if strings.TrimSpace(string(data)) != content {
		t.Errorf("Expected %s, but got %s", content, string(data))
	}
}

func TestSplitJSONData(t *testing.T) {
	users := `{"user_id": "1", "email": "user1@example.com", "email_verified": true}
{"user_id": "2", "email": "user2@example.com", "email_verified": true}
{"user_id": "3", "email": "user3@example.com", "email_verified": true}`

	data := []byte(users)

	reader := strings.NewReader(string(data))
	chunks, err := splitJSONData(reader, 100, true)
	if err != nil {
		t.Fatalf("Failed to split JSON data: %v", err)
	}

	if len(chunks) <= 1 {
		t.Fatalf("Expected more than 1 chunk, got %d", len(chunks))
	}

	for _, chunk := range chunks {
		if len(chunk) == 0 {
			t.Errorf("Chunk should not be empty")
		}
	}
}
