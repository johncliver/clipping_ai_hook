package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"clipping_ia_hook/internal/ui"
)

func ObtenerDuracionAudio(path string) (float64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		path,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe audio: %v\n%s", err, stderr.String())
	}
	s := strings.TrimSpace(stdout.String())
	d, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("duración inválida %q: %w", s, err)
	}
	if d < 0.1 {
		return 0, fmt.Errorf("audio demasiado corto (%.3fs)", d)
	}
	return d, nil
}

func ObtenerDimensionesVideo(path string) (int, int) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0",
		path,
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return 1920, 1080
	}
	s := strings.TrimSpace(stdout.String())
	parts := strings.Split(s, ",")
	if len(parts) < 2 {
		return 1920, 1080
	}
	w, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
	if w <= 0 || h <= 0 {
		return 1920, 1080
	}
	return w, h
}

func ConstruirComandoMusicVideo(videoBg, audioPath, assPath, output string,
	audioDur float64, srcW, srcH int) []string {

	targetW, targetH := 1080, 1920
	if srcW <= 0 || srcH <= 0 {
		srcW, srcH = 1920, 1080
	}
	cropH := srcH
	cropW := srcH * targetW / targetH
	if cropW > srcW {
		cropW = srcW
		cropH = srcW * targetH / targetW
	}
	if cropW%2 != 0 {
		cropW--
	}
	if cropH%2 != 0 {
		cropH--
	}
	cropX := (srcW - cropW) / 2
	if cropX < 0 {
		cropX = 0
	}
	cropFilter := fmt.Sprintf("crop=%d:%d:x=%d:y=0", cropW, cropH, cropX)
	scaleFilter := fmt.Sprintf("scale=%d:%d", targetW, targetH)
	var vf string
	if assPath != "" {
		ass := escapePathFFmpeg(assPath)
		vf = fmt.Sprintf("%s,%s,ass='%s'", cropFilter, scaleFilter, ass)
	} else {
		vf = fmt.Sprintf("%s,%s", cropFilter, scaleFilter)
	}
	return []string{
		"-y",
		"-stream_loop", "-1",
		"-i", videoBg,
		"-i", audioPath,
		"-t", fmt.Sprintf("%.3f", audioDur),
		"-vf", vf,
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		output,
	}
}

func guardarTranscripcionJSON(t *Transcripcion, path string) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func doMusicVideo(outDir string) bool {
	fmt.Println(ui.Col(ui.Cyan, "🎵 Video vertical con fondo + audio + subtítulos"))
	fmt.Println()

	bgPath := ui.Ask("Ruta al video de fondo (.mp4/.mov)")
	if bgPath == "" || !ui.Exists(bgPath) {
		ui.ErrLn("No existe: " + bgPath)
		return false
	}
	audioPath := ui.Ask("Ruta al audio (.mp3/.wav/.m4a)")
	if audioPath == "" || !ui.Exists(audioPath) {
		ui.ErrLn("No existe: " + audioPath)
		return false
	}

	audioDur, err := ObtenerDuracionAudio(audioPath)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	fmt.Println(ui.Col(ui.Gray, fmt.Sprintf("  ⏱️  Audio: %.2fs", audioDur)))

	srcW, srcH := ObtenerDimensionesVideo(bgPath)
	fmt.Println(ui.Col(ui.Gray, fmt.Sprintf("  📐 Video fondo: %dx%d", srcW, srcH)))

	transPath := filepath.Join(outDir, "music_video_transcripcion.json")
	var t *Transcripcion
	if ui.Exists(transPath) {
		if !ui.AskYes("Ya existe transcripción del audio. ¿Regenerar?") {
			t, err = loadTranscripcionJSON(transPath)
			if err != nil {
				ui.ErrLn(err.Error())
				return false
			}
			fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando transcripción existente"))
		}
	}
	if t == nil {
		fmt.Println(ui.Col(ui.Cyan, "🎙️  Transcribiendo audio con Whisper (puede tardar)..."))
		t, err = transcribirLocalConWhisper(audioPath)
		if err != nil {
			ui.ErrLn("whisper: " + err.Error())
			return false
		}
		if err := guardarTranscripcionJSON(t, transPath); err != nil {
			ui.WarnLn("guardando transcripción: " + err.Error())
		}
		ui.OKLn(fmt.Sprintf("%d segmentos → %s", len(t.Segmentos), transPath))
	}

	assPath := filepath.Join(outDir, "music_video.ass")
	if t != nil && len(t.Segmentos) > 0 {
		if err := GenerarASS(t, assPath); err != nil {
			ui.WarnLn("sin subtítulos: " + err.Error())
			assPath = ""
		} else {
			ui.OKLn("Subtítulos → " + assPath)
		}
	} else {
		ui.WarnLn("transcripción vacía, se omite subtítulo")
		assPath = ""
	}

	output := filepath.Join(outDir, "music_video.mp4")
	fmt.Println(ui.Col(ui.Cyan, "🎬 Renderizando video final..."))
	args := ConstruirComandoMusicVideo(bgPath, audioPath, assPath, output, audioDur, srcW, srcH)
	if err := EjecutarFFmpeg(args); err != nil {
		ui.ErrLn("ffmpeg: " + err.Error())
		return false
	}
	ui.OKLn("Video listo: " + output)
	return true
}
