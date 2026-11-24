package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"clipping_ia_hook/internal/ui"
)

// Volumen del audio del video de fondo cuando NO se silencia (0.0 - 1.0).
const VolumenFondoMezcla = 0.3

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

// tieneAudio indica si el archivo tiene al menos una pista de audio.
func tieneAudio(path string) bool {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_type",
		"-of", "csv=p=0",
		path,
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.Contains(stdout.String(), "audio")
}

// ══════════════════════════════════════════════════════════════
// Resolver ruta local o URL (YouTube) → archivo local
// ══════════════════════════════════════════════════════════════

func esURLWeb(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

func limpiarEntradaRuta(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return strings.TrimSpace(s)
}

func hashCorto(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// descargarYouTubeVideoFondo baja el video (H.264 + AAC en mp4) con audio incluido.
func descargarYouTubeVideoFondo(url, outDir string) (string, error) {
	base := filepath.Join(outDir, "mv_fondo_"+hashCorto(url))
	final := base + ".mp4"
	if ui.Exists(final) {
		fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando "+final))
		return final, nil
	}
	fmt.Println(ui.Col(ui.Cyan, "📥 Descargando video de fondo de YouTube…"))
	args := []string{
		"--no-playlist",
		"-f", "bv*[ext=mp4][vcodec^=avc1]+ba[ext=m4a]/bv*+ba/b",
		"--merge-output-format", "mp4",
		"-o", base + ".%(ext)s",
		url,
	}
	cmd := exec.Command("yt-dlp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp (video): %w", err)
	}
	if !ui.Exists(final) {
		return "", fmt.Errorf("yt-dlp terminó pero no encontré %s", final)
	}
	return final, nil
}

// descargarYouTubeAudio extrae solo el audio en mp3.
func descargarYouTubeAudio(url, outDir string) (string, error) {
	base := filepath.Join(outDir, "mv_audio_"+hashCorto(url))
	final := base + ".mp3"
	if ui.Exists(final) {
		fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando "+final))
		return final, nil
	}
	fmt.Println(ui.Col(ui.Cyan, "📥 Descargando audio de YouTube…"))
	args := []string{
		"--no-playlist",
		"-x", "--audio-format", "mp3", "--audio-quality", "0",
		"-o", base + ".%(ext)s",
		url,
	}
	cmd := exec.Command("yt-dlp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp (audio): %w", err)
	}
	if !ui.Exists(final) {
		return "", fmt.Errorf("yt-dlp terminó pero no encontré %s", final)
	}
	return final, nil
}

// resolverFuenteMedia devuelve una ruta local a partir de una ruta o una URL.
func resolverFuenteMedia(entrada, outDir string, soloAudio bool) (string, error) {
	entrada = limpiarEntradaRuta(entrada)
	if entrada == "" {
		return "", fmt.Errorf("entrada vacía")
	}
	if esURLWeb(entrada) {
		if soloAudio {
			return descargarYouTubeAudio(entrada, outDir)
		}
		return descargarYouTubeVideoFondo(entrada, outDir)
	}
	if !ui.Exists(entrada) {
		return "", fmt.Errorf("no existe: %s", entrada)
	}
	return entrada, nil
}

// ══════════════════════════════════════════════════════════════
// Comando FFmpeg
// ══════════════════════════════════════════════════════════════

func ConstruirComandoMusicVideo(videoBg, audioPath, assPath, output string,
	audioDur float64, srcW, srcH int, silenciarFondo bool) []string {

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

	// Fondo en silencio: solo suena el audio principal.
	if silenciarFondo {
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

	// Fondo con sonido: mezclar audio del fondo (bajito) + audio principal.
	fc := fmt.Sprintf(
		"[0:v]%s[vout];"+
			"[0:a]volume=%.2f[bg];"+
			"[1:a]volume=1.0[main];"+
			"[bg][main]amix=inputs=2:duration=longest:dropout_transition=0:normalize=0[aout]",
		vf, VolumenFondoMezcla)

	return []string{
		"-y",
		"-stream_loop", "-1",
		"-i", videoBg,
		"-i", audioPath,
		"-t", fmt.Sprintf("%.3f", audioDur),
		"-filter_complex", fc,
		"-map", "[vout]",
		"-map", "[aout]",
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

	// ── Video de fondo (ruta o YouTube) ──
	bgIn := ui.Ask("Ruta o URL de YouTube del video de fondo")
	bgPath, err := resolverFuenteMedia(bgIn, outDir, false)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}

	// ── ¿Silenciar el fondo? ──
	silenciarFondo := ui.AskYes("¿Dejar el video de fondo en silencio?")
	if !silenciarFondo && !tieneAudio(bgPath) {
		ui.WarnLn("el video de fondo no tiene audio, se queda en silencio")
		silenciarFondo = true
	}
	if silenciarFondo {
		fmt.Println(ui.Col(ui.Gray, "  🔇 Fondo en silencio"))
	} else {
		fmt.Println(ui.Col(ui.Gray, fmt.Sprintf(
			"  🔊 Fondo con sonido (mezclado al %.0f%%)", VolumenFondoMezcla*100)))
	}

	// ── Audio (ruta o YouTube) ──
	audioIn := ui.Ask("Ruta o URL de YouTube del audio")
	audioPath, err := resolverFuenteMedia(audioIn, outDir, true)
	if err != nil {
		ui.ErrLn(err.Error())
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
		audioURL := limpiarEntradaRuta(audioIn)
		if esURLWeb(audioURL) {
			// YouTube: subtítulos (en → es) y, si no hay, Whisper.
			t, err = obtenerTranscripcionYouTube(audioURL)
			if err != nil {
				ui.ErrLn("transcripción YouTube: " + err.Error())
				return false
			}
		} else {
			// Archivo local: Whisper.
			fmt.Println(ui.Col(ui.Cyan, "🎙️  Transcribiendo audio con Whisper (puede tardar)..."))
			t, err = transcribirLocalConWhisper(audioPath)
			if err != nil {
				ui.ErrLn("whisper: " + err.Error())
				return false
			}
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
	args := ConstruirComandoMusicVideo(bgPath, audioPath, assPath, output,
		audioDur, srcW, srcH, silenciarFondo)
	if err := EjecutarFFmpeg(args); err != nil {
		ui.ErrLn("ffmpeg: " + err.Error())
		return false
	}
	ui.OKLn("Video listo: " + output)
	return true
}
