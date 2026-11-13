package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// descargarVideoEn baja a outDir/video.mp4 con H.264 + AAC (compatibles con
// todas las plataformas al subir). yt-dlp invoca ffmpeg para el merge.
func descargarVideoEn(url, outDir string) (string, error) {
	outTpl := filepath.Join(outDir, "video.%(ext)s")

	cmd := exec.Command("yt-dlp",
		"-f", "bv*[vcodec^=avc1][ext=mp4]+ba[acodec^=mp4a]/bv*[ext=mp4]",
		"--merge-output-format", "mp4",
		"--no-playlist",
		"-o", outTpl,
		url,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp video: %w", err)
	}

	// yt-dlp decidió la extensión final (normalmente .mp4)
	matches, _ := filepath.Glob(filepath.Join(outDir, "video.*"))
	for _, m := range matches {
		switch strings.ToLower(filepath.Ext(m)) {
		case ".mp4", ".mkv", ".webm", ".mov":
			// Renombra a video.mp4 si quedó distinto (fallback imprevisto)
			if !strings.HasSuffix(strings.ToLower(m), ".mp4") {
				final := filepath.Join(outDir, "video.mp4")
				_ = os.Remove(final)
				if err := os.Rename(m, final); err == nil {
					return final, nil
				}
			}
			return m, nil
		}
	}
	return "", fmt.Errorf("yt-dlp terminó pero no encontré video.* en %s", outDir)
}
