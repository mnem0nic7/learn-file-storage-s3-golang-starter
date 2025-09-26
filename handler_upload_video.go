package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = int64(1 << 30)
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

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

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse multipart form", err)
		return
	}

	file, header, err := r.FormFile("video")
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

	const sniffLen = 512
	sniffBuf := make([]byte, sniffLen)
	n, err := io.ReadFull(file, sniffBuf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		respondWithError(w, http.StatusInternalServerError, "Couldn't read video file", err)
		return
	}
	sniffBytes := sniffBuf[:n]

	if len(sniffBytes) == 0 {
		respondWithError(w, http.StatusBadRequest, "Empty video file", nil)
		return
	}

	if mediaType == "" || mediaType == "application/octet-stream" {
		detectedType := http.DetectContentType(sniffBytes)
		if detectedType != "" {
			mediaType = detectedType
		}
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Only MP4 videos are supported", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-upload-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't create temp file", err)
		return
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())

	if _, err := io.Copy(tempFile, io.MultiReader(bytes.NewReader(sniffBytes), file)); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't save temp video file", err)
		return
	}

	if err := tempFile.Sync(); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't flush temp video file", err)
		return
	}

	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't determine video aspect ratio", err)
		return
	}

	if err := tempFile.Close(); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't close temp file", err)
		return
	}

	processedPath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't process video for fast start", err)
		return
	}
	defer os.Remove(processedPath)

	processedFile, err := os.Open(processedPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't open processed video", err)
		return
	}
	defer processedFile.Close()

	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't generate video key", err)
		return
	}
	baseKey := hex.EncodeToString(randomBytes) + ".mp4"

	prefix := "other/"
	switch aspectRatio {
	case "16:9":
		prefix = "landscape/"
	case "9:16":
		prefix = "portrait/"
	}

	objectKey := prefix + baseKey

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(objectKey),
		Body:        processedFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video", err)
		return
	}

	cfBase := strings.TrimSpace(cfg.s3CfDistribution)
	if cfBase == "" {
		respondWithError(w, http.StatusInternalServerError, "CloudFront distribution not configured", nil)
		return
	}
	if !strings.HasPrefix(cfBase, "http://") && !strings.HasPrefix(cfBase, "https://") {
		cfBase = "https://" + cfBase
	}
	cfBase = strings.TrimRight(cfBase, "/")
	videoURL := fmt.Sprintf("%s/%s", cfBase, objectKey)
	video.VideoURL = &videoURL

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
