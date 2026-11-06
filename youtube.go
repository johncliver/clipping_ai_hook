package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// descargarVideoEn baja a outDir/video.mp4 (persistente).
func descargarVideoEn(url, outDir string) (string, error) {
	outPath := filepath.Join(outDir, "video.mp4")
	cmd := exec.Command("yt-dlp",
		"-f", "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/best[height<=1080][ext=mp4]/best",
		"--merge-output-format", "mp4",
		"-o", outPath,
		url,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp video: %w", err)
	}
	return outPath, nil
}
