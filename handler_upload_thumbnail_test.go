package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func TestHandlerUploadThumbnailStoresFile(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	dbClient, err := database.NewClient(dbPath)
	if err != nil {
		t.Fatalf("failed to create db client: %v", err)
	}

	cfg := apiConfig{
		db:         dbClient,
		jwtSecret:  "test-secret",
		assetsRoot: tempDir,
		port:       "8091",
	}

	hashedPassword, err := auth.HashPassword("super-secret")
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	user, err := cfg.db.CreateUser(database.CreateUserParams{
		Email:    "thumb@example.com",
		Password: hashedPassword,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	video, err := cfg.db.CreateVideo(database.CreateVideoParams{
		Title:       "Test Video",
		Description: "A sample video",
		UserID:      user.ID,
	})
	if err != nil {
		t.Fatalf("failed to create video: %v", err)
	}

	token, err := auth.MakeJWT(user.ID, cfg.jwtSecret, time.Hour)
	if err != nil {
		t.Fatalf("failed to create jwt: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileWriter, err := writer.CreateFormFile("thumbnail", "thumb.bin")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}

	sampleData := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := fileWriter.Write(sampleData); err != nil {
		t.Fatalf("failed to write sample data: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/thumbnail_upload/"+video.ID.String(), body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rr := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/thumbnail_upload/{videoID}", cfg.handlerUploadThumbnail)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", rr.Code)
	}

	var got database.Video
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if got.ThumbnailURL == nil {
		t.Fatalf("expected thumbnail url in response")
	}

	parsedURL, err := url.Parse(*got.ThumbnailURL)
	if err != nil {
		t.Fatalf("invalid thumbnail url: %v", err)
	}

	expectedHost := fmt.Sprintf("localhost:%s", cfg.port)
	if parsedURL.Host != expectedHost {
		t.Fatalf("unexpected host: want %s got %s", expectedHost, parsedURL.Host)
	}

	if parsedURL.Scheme != "http" {
		t.Fatalf("unexpected scheme: want http got %s", parsedURL.Scheme)
	}

	if !strings.HasPrefix(parsedURL.Path, "/assets/") {
		t.Fatalf("thumbnail path should be under /assets, got %s", parsedURL.Path)
	}

	thumbnailFile := path.Base(parsedURL.Path)
	if filepath.Ext(thumbnailFile) != ".png" {
		t.Fatalf("expected png extension, got %s", filepath.Ext(thumbnailFile))
	}

	if strings.Contains(thumbnailFile, video.ID.String()) {
		t.Fatalf("thumbnail filename should not contain video ID, got %s", thumbnailFile)
	}

	stored, err := cfg.db.GetVideo(video.ID)
	if err != nil {
		t.Fatalf("failed to reload video: %v", err)
	}

	if stored.ThumbnailURL == nil {
		t.Fatalf("expected stored thumbnail url")
	}

	if *stored.ThumbnailURL != *got.ThumbnailURL {
		t.Fatalf("stored thumbnail url mismatch: want %s got %s", *got.ThumbnailURL, *stored.ThumbnailURL)
	}

	thumbnailPath := filepath.Join(cfg.assetsRoot, thumbnailFile)
	if info, err := os.Stat(thumbnailPath); err != nil {
		t.Fatalf("expected thumbnail file at %s: %v", thumbnailPath, err)
	} else if info.Size() == 0 {
		t.Fatalf("expected thumbnail file to be non-empty")
	}
}
