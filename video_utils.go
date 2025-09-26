package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strings"
)

type ffprobeStream struct {
	CodecType string `json:"codec_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("ffprobe failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("ffprobe failed: %w", err)
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &probe); err != nil {
		return "", fmt.Errorf("couldn't parse ffprobe output: %w", err)
	}

	var width, height int
	for _, stream := range probe.Streams {
		if stream.CodecType == "video" && stream.Width > 0 && stream.Height > 0 {
			width = stream.Width
			height = stream.Height
			break
		}
	}

	if width == 0 || height == 0 {
		return "", fmt.Errorf("video dimensions not found in ffprobe output")
	}

	ratio := float64(width) / float64(height)

	const (
		landscapeRatio = 16.0 / 9.0
		portraitRatio  = 9.0 / 16.0
		tolerance      = 0.02
	)

	switch {
	case math.Abs(ratio-landscapeRatio) <= tolerance:
		return "16:9", nil
	case math.Abs(ratio-portraitRatio) <= tolerance:
		return "9:16", nil
	default:
		return "other", nil
	}
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + ".processing"

	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-i", filePath,
		"-c", "copy",
		"-movflags", "faststart",
		"-f", "mp4",
		outputPath,
	)

	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return "", fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return "", fmt.Errorf("ffmpeg failed: %w", err)
	}

	return outputPath, nil
}
