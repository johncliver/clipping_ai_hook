package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"clipping_ia_hook/internal/ui"
)

// ══════════════════════════════════════════════════════════════
// END VIDEO GLOBAL: un video completo pegado al final de cada clip
// ══════════════════════════════════════════════════════════════

var EndVideoPath string // ruta al .mp4 ya normalizado; "" = desactivado

// AplicarEndEnClip es el punto único de entrada: si hay end video lo usa,
// si no, cae al end card de imagen. Reemplaza las llamadas a AplicarEndCardEnClip.
func AplicarEndEnClip(input, output string) error {
	if EndVideoPath != "" {
		return AplicarEndVideoEnClip(input, output)
	}
	return AplicarEndCardEnClip(input, output)
}

// ── Utilidades ffprobe ───────────────────────────────────────

func probeCodec(path, stream string) string {
	cmd := exec.Command("ffprobe", "-v", "error",
		"-select_streams", stream,
		"-show_entries", "stream=codec_name",
		"-of", "csv=p=0", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(out.String(), "\n")[0])
}

func duracionMedia(path string) float64 {
	cmd := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0", path)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(out.String()), 64)
	return v
}

// AsegurarMP4 devuelve una ruta a un .mp4 (H.264 + AAC). Si el archivo ya
// lo es, lo devuelve tal cual; si no, lo convierte con ffmpeg a outDir.
func AsegurarMP4(path, outDir string) (string, error) {
	vcodec := probeCodec(path, "v:0")
	if vcodec == "" {
		return "", fmt.Errorf("el archivo no tiene stream de video")
	}
	acodec := probeCodec(path, "a:0")
	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".mp4" && vcodec == "h264" && (acodec == "" || acodec == "aac") {
		return path, nil // ya es compatible
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	out := filepath.Join(outDir, "endvideo_"+base+".mp4")

	ui.WarnLn(fmt.Sprintf("Formato %s (%s/%s) → convirtiendo a .mp4…", ext, vcodec, acodec))
	args := []string{
		"-y", "-i", path,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000",
		"-movflags", "+faststart",
		out,
	}
	if err := EjecutarFFmpeg(args); err != nil {
		return "", fmt.Errorf("ffmpeg al convertir: %w", err)
	}
	return out, nil
}

// ── Configuración (menú) ─────────────────────────────────────

func configurarEndVideo(outDir string) {
	fmt.Println(ui.Col(ui.Cyan, "🎞️  End video (video al final de cada clip)"))
	fmt.Println()
	if EndVideoPath == "" {
		fmt.Println(ui.Col(ui.Gray, "  Estado: DESACTIVADO"))
	} else {
		fmt.Println(ui.Col(ui.Green, "  Estado: ACTIVADO"))
		fmt.Println(ui.Col(ui.Gray, "  Video: "+EndVideoPath))
		fmt.Println(ui.Col(ui.Gray, fmt.Sprintf("  Duración: %.1fs", duracionMedia(EndVideoPath))))
	}
	fmt.Println()
	fmt.Println(ui.Col(ui.White, "  ¿Qué hacer?"))
	fmt.Println(ui.Col(ui.Gray, "  1) Activar / cambiar video"))
	fmt.Println(ui.Col(ui.Gray, "  2) Desactivar"))
	op := ui.Ask("Elige 1-2 (vacío = volver)")

	switch op {
	case "1":
		p := ui.Ask("Ruta al video (cualquier formato)")
		if p == "" || !ui.Exists(p) {
			ui.ErrLn("No existe: " + p)
			return
		}
		mp4, err := AsegurarMP4(p, outDir)
		if err != nil {
			ui.ErrLn(err.Error())
			return
		}
		EndVideoPath = mp4
		ui.OKLn(fmt.Sprintf("End video ACTIVADO → %s (%.1fs)", mp4, duracionMedia(mp4)))
		if EndCardImagen != "" {
			ui.WarnLn("El end video tiene prioridad sobre el end card de imagen")
		}
	case "2":
		EndVideoPath = ""
		ui.OKLn("End video DESACTIVADO")
	}
}

func estadoEndVideo() string {
	if EndVideoPath == "" {
		return ui.Col(ui.Gray, "OFF")
	}
	return ui.Col(ui.Green+ui.Bold, fmt.Sprintf("ON · %.1fs · %s",
		duracionMedia(EndVideoPath), filepath.Base(EndVideoPath)))
}

// ── Aplicar a un clip ────────────────────────────────────────

func AplicarEndVideoEnClip(input, output string) error {
	if EndVideoPath == "" {
		return nil
	}
	srcW, srcH := ObtenerDimensionesVideo(input)
	fps := ObtenerFPSVideo(input)
	args := ConstruirComandoEndVideo(input, output, EndVideoPath,
		srcW, srcH, fps,
		duracionMedia(input), duracionMedia(EndVideoPath),
		tieneAudio(input), tieneAudio(EndVideoPath))
	return EjecutarFFmpeg(args)
}

// ConstruirComandoEndVideo pega `endVideo` después de `input`.
// El segundo video se escala/pad al tamaño del clip y se normalizan fps,
// SAR, pixel format y audio (48k estéreo). Si uno de los dos no tiene
// audio se le genera silencio para que concat no falle.
func ConstruirComandoEndVideo(input, output, endVideo string,
	srcW, srcH int, fps, clipDur, endDur float64,
	clipAudio, endAudio bool) []string {

	if srcW <= 0 || srcH <= 0 {
		srcW, srcH = 1080, 1920
	}
	if fps <= 0 {
		fps = 30
	}

	args := []string{"-y", "-i", input, "-i", endVideo}
	next := 2
	conAudio := clipAudio || endAudio

	clipA, endA := "[0:a]", "[1:a]"
	if conAudio {
		if !clipAudio {
			args = append(args, "-f", "lavfi", "-t", fmt.Sprintf("%.3f", clipDur),
				"-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
			clipA = fmt.Sprintf("[%d:a]", next)
			next++
		}
		if !endAudio {
			args = append(args, "-f", "lavfi", "-t", fmt.Sprintf("%.3f", endDur),
				"-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
			endA = fmt.Sprintf("[%d:a]", next)
			next++
		}
	}

	filter := fmt.Sprintf(
		"[0:v]fps=%.3f,setsar=1,format=yuv420p,setpts=PTS-STARTPTS[v0];"+
			"[1:v]fps=%.3f,scale=%d:%d:force_original_aspect_ratio=decrease,"+
			"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,"+
			"setsar=1,format=yuv420p,setpts=PTS-STARTPTS[v1];"+
			"[v0][v1]concat=n=2:v=1:a=0,format=yuv420p[vout]",
		fps, fps, srcW, srcH, srcW, srcH)

	maps := []string{"-map", "[vout]"}
	if conAudio {
		filter += ";" +
			clipA + "aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,asetpts=PTS-STARTPTS[a0];" +
			endA + "aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,asetpts=PTS-STARTPTS[a1];" +
			"[a0][a1]concat=n=2:v=0:a=1[aout]"
		maps = append(maps, "-map", "[aout]")
	}

	args = append(args, "-filter_complex", filter)
	args = append(args, maps...)
	args = append(args,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		output,
	)
	return args
}

// ── Handler: añadir end video a clips ya existentes ──────────

func doEndVideo(outDir string) bool {
	fmt.Println(ui.Col(ui.Cyan, "🎞️  Añadir video al final de clips existentes"))
	fmt.Println()

	endPath := EndVideoPath
	if endPath == "" {
		p := ui.Ask("Ruta al video que irá al final (cualquier formato)")
		if p == "" || !ui.Exists(p) {
			ui.ErrLn("No existe: " + p)
			return false
		}
		mp4, err := AsegurarMP4(p, outDir)
		if err != nil {
			ui.ErrLn(err.Error())
			return false
		}
		endPath = mp4
	} else {
		fmt.Println(ui.Col(ui.Gray, "  Usando end video configurado: "+endPath))
	}

	fmt.Println(ui.Col(ui.White, "¿A qué video(s)?"))
	fmt.Println(ui.Col(ui.Gray, "  1) Todos los clip_*.mp4 en "+outDir+"/"))
	fmt.Println(ui.Col(ui.Gray, "  2) Un archivo específico"))
	fmt.Println(ui.Col(ui.Gray, "  3) Todos los .mp4 de una carpeta"))
	target := ui.Ask("Elige 1, 2 o 3 (vacío = 1)")

	var files []string
	switch target {
	case "2":
		p := ui.Ask("Ruta al video")
		if p == "" || !ui.Exists(p) {
			ui.ErrLn("No existe: " + p)
			return false
		}
		files = []string{p}
	case "3":
		dir := ui.Ask("Carpeta")
		raw, _ := filepath.Glob(filepath.Join(dir, "*.mp4"))
		for _, f := range raw {
			if !isAlreadyEndCapped(f) && f != endPath {
				files = append(files, f)
			}
		}
	default:
		raw, _ := filepath.Glob(filepath.Join(outDir, "clip_*.mp4"))
		for _, f := range raw {
			if !isAlreadyEndCapped(f) {
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		ui.ErrLn("No hay videos para procesar (¿ya tienen sufijo _end?)")
		return false
	}

	endDur := duracionMedia(endPath)
	endAudio := tieneAudio(endPath)
	ok := 0
	for i, f := range files {
		srcW, srcH := ObtenerDimensionesVideo(f)
		fps := ObtenerFPSVideo(f)
		e := filepath.Ext(f)
		output := strings.TrimSuffix(f, e) + "_end" + e

		fmt.Printf("  [%d/%d] %s → %s (+%.1fs, %dx%d)\n",
			i+1, len(files), filepath.Base(f), filepath.Base(output), endDur, srcW, srcH)

		args := ConstruirComandoEndVideo(f, output, endPath, srcW, srcH, fps,
			duracionMedia(f), endDur, tieneAudio(f), endAudio)
		if err := EjecutarFFmpeg(args); err != nil {
			ui.WarnLn("ffmpeg: " + err.Error())
			continue
		}
		ok++
	}
	if ok == 0 {
		ui.ErrLn("Ningún video se procesó correctamente")
		return false
	}
	ui.OKLn(fmt.Sprintf("%d/%d videos con end video", ok, len(files)))
	return true
}