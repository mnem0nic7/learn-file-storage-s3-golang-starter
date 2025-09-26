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

func TestHandlerVideosRetrieveReturnsAssetURL(t *testing.T) {
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
		Email:    "dataurl@example.com",
		Password: hashedPassword,
	})
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	video, err := cfg.db.CreateVideo(database.CreateVideoParams{
		Title:       "Data URL Thumbnail",
		Description: "Stored as base64",
		UserID:      user.ID,
	})
	if err != nil {
		t.Fatalf("failed to create video: %v", err)
	}

	token, err := auth.MakeJWT(user.ID, cfg.jwtSecret, time.Hour)
	if err != nil {
		t.Fatalf("failed to create jwt: %v", err)
	}

	// Upload thumbnail to populate the database with an assets URL
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileWriter, err := writer.CreateFormFile("thumbnail", "thumb.png")
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

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/thumbnail_upload/"+video.ID.String(), body)
	uploadReq.Header.Set("Authorization", "Bearer "+token)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())

	uploadRR := httptest.NewRecorder()
	uploadMux := http.NewServeMux()
	uploadMux.HandleFunc("POST /api/thumbnail_upload/{videoID}", cfg.handlerUploadThumbnail)
	uploadMux.ServeHTTP(uploadRR, uploadReq)

	if uploadRR.Code != http.StatusOK {
		t.Fatalf("expected upload status OK, got %d", uploadRR.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/videos", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rr := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/videos", cfg.handlerVideosRetrieve)
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status OK, got %d", rr.Code)
	}

	var videos []database.Video
	if err := json.Unmarshal(rr.Body.Bytes(), &videos); err != nil {
		t.Fatalf("failed to unmarshal videos: %v", err)
	}

	if len(videos) != 1 {
		t.Fatalf("expected 1 video, got %d", len(videos))
	}

	thumbnailURL := videos[0].ThumbnailURL
	if thumbnailURL == nil {
		t.Fatalf("expected thumbnail url in response")
	}

	parsedURL, err := url.Parse(*thumbnailURL)
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

	updatedVideo, err := cfg.db.GetVideo(video.ID)
	if err != nil {
		t.Fatalf("failed to reload video: %v", err)
	}

	if updatedVideo.ThumbnailURL == nil {
		t.Fatalf("expected persisted thumbnail url")
	}

	if *updatedVideo.ThumbnailURL != *thumbnailURL {
		t.Fatalf("persisted thumbnail url mismatch; want %s got %s", *thumbnailURL, *updatedVideo.ThumbnailURL)
	}

	thumbnailPath := filepath.Join(cfg.assetsRoot, thumbnailFile)
	if info, err := os.Stat(thumbnailPath); err != nil {
		t.Fatalf("expected thumbnail file at %s: %v", thumbnailPath, err)
	} else if info.Size() == 0 {
		t.Fatalf("expected thumbnail file to be non-empty")
	}
}
