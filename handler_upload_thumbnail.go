package main

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func getFileExtensionFromContentType(contentType string) string {
	// Clean up the content type (remove charset and other params)
	contentType = strings.Split(contentType, ";")[0]
	contentType = strings.TrimSpace(contentType)
	
	// Get extensions from MIME type
	exts, err := mime.ExtensionsByType(contentType)
	if err != nil || len(exts) == 0 {
		// Fallback for common types
		switch contentType {
		case "image/jpeg":
			return ".jpg"
		case "image/png":
			return ".png"
		case "image/gif":
			return ".gif"
		case "image/webp":
			return ".webp"
		case "application/pdf":
			return ".pdf"
		default:
			return ".bin" // fallback extension
		}
	}
	
	// Return the first (most common) extension
	return exts[0]
}

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// Parse multipart form with 10MB max memory
	err = r.ParseMultipartForm(10 << 20) // 10MB
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to parse multipart form", err)
		return
	}

	// Get the file from the form
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to get thumbnail file", err)
		return
	}
	defer file.Close()

	// Get the content type
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		// Fallback to detect content type from file
		buffer := make([]byte, 512)
		_, err := file.Read(buffer)
		if err != nil {
			respondWithError(w, http.StatusBadRequest, "Failed to read file for type detection", err)
			return
		}
		contentType = http.DetectContentType(buffer)
		
		// Reset file pointer to beginning
		_, err = file.Seek(0, 0)
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Failed to reset file pointer", err)
			return
		}
	}

	// Get file extension from content type
	fileExtension := getFileExtensionFromContentType(contentType)

	// Create file path: /assets/<videoID>.<extension>
	fileName := fmt.Sprintf("%s%s", videoID.String(), fileExtension)
	filePath := filepath.Join(cfg.assetsRoot, fileName)

	// Create the file on disk
	destFile, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to create file", err)
		return
	}
	defer destFile.Close()

	// Copy uploaded file contents to destination file
	_, err = io.Copy(destFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to save file", err)
		return
	}

	// Get the video from database to update thumbnail URL
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}

	// Update thumbnail URL to point to the file server endpoint
	thumbnailURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, fileName)
	video.ThumbnailURL = &thumbnailURL

	// Update video in database
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update video", err)
		return
	}

	fmt.Printf("Thumbnail saved to: %s\n", filePath)
	fmt.Printf("Thumbnail URL: %s\n", thumbnailURL)

	respondWithJSON(w, http.StatusOK, struct{}{})
}
