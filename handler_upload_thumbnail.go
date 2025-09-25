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

	const maxMemory = 10 << 20
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse multipart form", err)
		return
	}

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaTypeHeader := header.Header.Get("Content-Type")
	if mediaTypeHeader == "" {
		buffer := make([]byte, 512)
		if _, err := file.Read(buffer); err != nil {
			respondWithError(w, http.StatusBadRequest, "Failed to read file for type detection", err)
			return
		}

		mediaTypeHeader = http.DetectContentType(buffer)

		if _, err := file.Seek(0, 0); err != nil {
			respondWithError(w, http.StatusInternalServerError, "Failed to reset file pointer", err)
			return
		}
	}

	mediaType, _, err := mime.ParseMediaType(mediaTypeHeader)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse Content-Type", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video", err)
		return
	}
	if video.ID == uuid.Nil {
		respondWithError(w, http.StatusNotFound, "Video not found", nil)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You can't modify this video", nil)
		return
	}

	var fileExt string
	if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
		fileExt = exts[0]
	}
	if fileExt == "" {
		fileExt = getFileExtensionFromContentType(mediaType)
	}
	if fileExt == "" {
		fileExt = strings.ToLower(filepath.Ext(header.Filename))
	}
	if fileExt == "" {
		respondWithError(w, http.StatusBadRequest, "Couldn't determine file extension", nil)
		return
	}

	filename := fmt.Sprintf("%s%s", videoID.String(), fileExt)
	assetPath := filepath.Join(cfg.assetsRoot, filename)

	dst, err := os.Create(assetPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create thumbnail file", err)
		return
	}

	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.Remove(assetPath)
		respondWithError(w, http.StatusInternalServerError, "Couldn't write thumbnail file", err)
		return
	}

	if err := dst.Close(); err != nil {
		os.Remove(assetPath)
		respondWithError(w, http.StatusInternalServerError, "Couldn't close thumbnail file", err)
		return
	}

	thumbnailURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, filename)
	video.ThumbnailURL = &thumbnailURL

	if err := cfg.db.UpdateVideo(video); err != nil {
		os.Remove(assetPath)
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}

	updatedVideo, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't retrieve updated video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, updatedVideo)
}
