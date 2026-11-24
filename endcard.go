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
// END CARD GLOBAL (aplicado automáticamente durante la generación de clips)
// ══════════════════════════════════════════════════════════════

var EndCardImagen string  // ruta a la imagen; "" = desactivado
var EndCardDuracion = 3.0 // segundos al final de cada clip

// configurarEndCard permite activar/cambiar/desactivar la imagen y la
// duración que se aplicarán a TODOS los clips generados desde ahora.
func configurarEndCard() {
	fmt.Println(ui.Col(ui.Cyan, "🖼️  End card para clips"))
	fmt.Println()
	if EndCardImagen == "" {
		fmt.Println(ui.Col(ui.Gray, "  Estado: DESACTIVADO"))
	} else {
		fmt.Println(ui.Col(ui.Green, "  Estado: ACTIVADO"))
		fmt.Println(ui.Col(ui.Gray, "  Imagen:    "+EndCardImagen))
		fmt.Println(ui.Col(ui.Gray, fmt.Sprintf("  Duración:  %.1fs al final de cada clip", EndCardDuracion)))
	}
	fmt.Println()
	fmt.Println(ui.Col(ui.White, "  ¿Qué hacer?"))
	fmt.Println(ui.Col(ui.Gray, "  1) Activar / cambiar imagen"))
	fmt.Println(ui.Col(ui.Gray, "  2) Desactivar"))
	fmt.Println(ui.Col(ui.Gray, "  3) Cambiar duración"))
	op := ui.Ask("Elige 1-3 (vacío = volver)")

	switch op {
	case "1":
		p := ui.Ask("Ruta a la imagen (.png/.jpg)")
		if p == "" || !ui.Exists(p) {
			ui.ErrLn("No existe: " + p)
			return
		}
		EndCardImagen = p
		dStr := ui.Ask("Duración al final en segundos (vacío = 3)")
		if dStr != "" {
			if v, err := strconv.ParseFloat(dStr, 64); err == nil && v > 0 {
				EndCardDuracion = v
			}
		}
		ui.OKLn(fmt.Sprintf("End card ACTIVADO → %s (%.1fs)", EndCardImagen, EndCardDuracion))
		ui.OKLn("Se aplicará automáticamente a cada clip generado (opciones 6 y 7)")
	case "2":
		EndCardImagen = ""
		ui.OKLn("End card DESACTIVADO")
	case "3":
		dStr := ui.Ask(fmt.Sprintf("Nueva duración en segundos (actual %.1f)", EndCardDuracion))
		if v, err := strconv.ParseFloat(dStr, 64); err == nil && v > 0 {
			EndCardDuracion = v
			ui.OKLn(fmt.Sprintf("Duración = %.1fs", EndCardDuracion))
		}
	}
}

// AplicarEndCardEnClip añade la imagen al final de un clip ya generado.
// Devuelve nil si EndCardImagen está vacío (no-op).
func AplicarEndCardEnClip(input, output string) error {
	if EndCardImagen == "" {
		return nil // desactivado
	}
	srcW, srcH := ObtenerDimensionesVideo(input)
	fps := ObtenerFPSVideo(input)
	conAudio := tieneAudio(input)
	args := ConstruirComandoEndCard(input, output, EndCardImagen,
		srcW, srcH, fps, EndCardDuracion, conAudio)
	return EjecutarFFmpeg(args)
}

// ConstruirComandoEndCard arma el ffmpeg para añadir una imagen al final del video.
//
//	duracion : segundos que la imagen se muestra al final
//	conAudio : si el video original tiene pista de audio
func ConstruirComandoEndCard(input, output, imgPath string,
	srcW, srcH int, fps float64, duracion float64, conAudio bool) []string {

	if srcW <= 0 || srcH <= 0 {
		srcW, srcH = 1080, 1920
	}
	if fps <= 0 {
		fps = 30
	}

	var filter string
	var maps []string

	if conAudio {
		filter = fmt.Sprintf(
			"[0:v]fps=%.3f,setsar=1,format=yuv420p,setpts=PTS-STARTPTS[v0];"+
				"[1:v]fps=%.3f,scale=%d:%d:force_original_aspect_ratio=decrease,"+
				"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,"+
				"setsar=1,format=yuv420p,setpts=PTS-STARTPTS[v1];"+
				"[v0][v1]concat=n=2:v=1:a=0,format=yuv420p[vout];"+
				"[0:a]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,"+
				"asetpts=PTS-STARTPTS[a0];"+
				"[2:a]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,"+
				"asetpts=PTS-STARTPTS[a1];"+
				"[a0][a1]concat=n=2:v=0:a=1[aout]",
			fps, fps, srcW, srcH, srcW, srcH)
		maps = []string{"-map", "[vout]", "-map", "[aout]"}
	} else {
		filter = fmt.Sprintf(
			"[0:v]fps=%.3f,setsar=1,format=yuv420p,setpts=PTS-STARTPTS[v0];"+
				"[1:v]fps=%.3f,scale=%d:%d:force_original_aspect_ratio=decrease,"+
				"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,"+
				"setsar=1,format=yuv420p,setpts=PTS-STARTPTS[v1];"+
				"[v0][v1]concat=n=2:v=1:a=0,format=yuv420p[vout]",
			fps, fps, srcW, srcH, srcW, srcH)
		maps = []string{"-map", "[vout]"}
	}

	args := []string{
		"-y",
		"-i", input,
		"-loop", "1", "-t", fmt.Sprintf("%.3f", duracion), "-i", imgPath,
	}
	if conAudio {
		args = append(args,
			"-f", "lavfi", "-t", fmt.Sprintf("%.3f", duracion),
			"-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
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

// ObtenerFPSVideo lee el fps del primer stream de video (parsea "30000/1001" etc).
func ObtenerFPSVideo(path string) float64 {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate",
		"-of", "csv=p=0",
		path,
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 30
	}
	s := strings.TrimSpace(out.String())
	parts := strings.Split(s, "/")
	if len(parts) == 2 {
		num, err1 := strconv.ParseFloat(parts[0], 64)
		den, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 == nil && err2 == nil && den > 0 {
			return num / den
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		return f
	}
	return 30
}

// tieneAudio devuelve true si el archivo tiene al menos un stream de audio.
// func tieneAudio(path string) bool {
// 	cmd := exec.Command("ffprobe",
// 		"-v", "error",
// 		"-select_streams", "a:0",
// 		"-show_entries", "stream=codec_type",
// 		"-of", "csv=p=0",
// 		path,
// 	)
// 	var out bytes.Buffer
// 	cmd.Stdout = &out
// 	if err := cmd.Run(); err != nil {
// 		return false
// 	}
// 	return strings.TrimSpace(out.String()) == "audio"
// }

// isAlreadyEndCapped detecta si el archivo ya tiene el sufijo _end
// para no re-procesar en bucle al apuntar a una carpeta.
func isAlreadyEndCapped(path string) bool {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.HasSuffix(base, "_end")
}

// doEndCard: handler del menú (opción 14).
func doEndCard(outDir string) bool {
	fmt.Println(ui.Col(ui.Cyan, "🖼️  Añadir imagen al final del video (end card)"))
	fmt.Println()

	// 1) Imagen
	imgPath := ui.Ask("Ruta a la imagen (.png/.jpg) que irá al final")
	if imgPath == "" || !ui.Exists(imgPath) {
		ui.ErrLn("No existe: " + imgPath)
		return false
	}
	ext := strings.ToLower(filepath.Ext(imgPath))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".bmp":
	default:
		ui.WarnLn("Extensión poco común para end card: " + ext)
	}

	// 2) Destino
	fmt.Println(ui.Col(ui.White, "¿A qué video añadir la imagen al final?"))
	fmt.Println(ui.Col(ui.Gray, "  1) Todos los clip_*.mp4 en "+outDir+"/"))
	fmt.Println(ui.Col(ui.Gray, "  2) Un archivo de video específico"))
	fmt.Println(ui.Col(ui.Gray, "  3) Todos los .mp4 de una carpeta"))
	target := ui.Ask("Elige 1, 2 o 3 (vacío = 1)")
	if target == "" {
		target = "1"
	}

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
			if !isAlreadyEndCapped(f) {
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

	// 3) Duración de la imagen
	durStr := ui.Ask("Duración de la imagen al final en segundos (vacío = 3)")
	duracion := 3.0
	if durStr != "" {
		if v, err := strconv.ParseFloat(durStr, 64); err == nil && v > 0 {
			duracion = v
		}
	}

	// 4) Procesar
	fmt.Println()
	ok := 0
	for i, f := range files {
		srcW, srcH := ObtenerDimensionesVideo(f)
		fps := ObtenerFPSVideo(f)
		conAudio := tieneAudio(f)
		e := filepath.Ext(f)
		base := strings.TrimSuffix(f, e)
		output := base + "_end" + e

		fmt.Printf("  [%d/%d] %s → %s (%.1fs, %dx%d, fps=%.2f, audio=%v)\n",
			i+1, len(files), filepath.Base(f), filepath.Base(output),
			duracion, srcW, srcH, fps, conAudio)

		args := ConstruirComandoEndCard(f, output, imgPath, srcW, srcH, fps, duracion, conAudio)
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
	ui.OKLn(fmt.Sprintf("%d/%d videos con end card", ok, len(files)))
	return true
}
