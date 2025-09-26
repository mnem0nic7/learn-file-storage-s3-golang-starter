package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
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

	mediaType := header.Header.Get("Content-Type")
	if mediaType != "" {
		if parsed, _, err := mime.ParseMediaType(mediaType); err == nil {
			mediaType = parsed
		}
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

	const sniffLen = 512
	sniffBuf := make([]byte, sniffLen)
	n, err := io.ReadFull(file, sniffBuf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		respondWithError(w, http.StatusInternalServerError, "Couldn't read thumbnail file", err)
		return
	}
	sniffBytes := sniffBuf[:n]

	if len(sniffBytes) == 0 {
		respondWithError(w, http.StatusBadRequest, "Empty thumbnail file", nil)
		return
	}

	if mediaType == "" || mediaType == "application/octet-stream" {
		detectedType := http.DetectContentType(sniffBytes)
		if detectedType != "" {
			mediaType = detectedType
		}
	}

	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Couldn't determine media type", nil)
		return
	}

	exts, _ := mime.ExtensionsByType(mediaType)
	ext := ""
	if len(exts) > 0 {
		ext = exts[0]
	}
	if ext == "" {
		if idx := strings.Index(mediaType, "/"); idx != -1 && idx < len(mediaType)-1 {
			ext = "." + mediaType[idx+1:]
		}
	}
	if ext == "" {
		respondWithError(w, http.StatusBadRequest, "Couldn't determine file extension", nil)
		return
	}
	ext = strings.ToLower(ext)

	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't generate thumbnail name", err)
		return
	}
	randomName := base64.RawURLEncoding.EncodeToString(randomBytes)
	destName := randomName + ext
	destPath := filepath.Join(cfg.assetsRoot, destName)

	destFile, err := os.Create(destPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create thumbnail file", err)
		return
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, io.MultiReader(bytes.NewReader(sniffBytes), file))
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't save thumbnail file", err)
		return
	}

	url := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, destName)
	video.ThumbnailURL = &url

	if err := cfg.db.UpdateVideo(video); err != nil {
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
