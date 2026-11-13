package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"clipping_ia_hook/internal/ui"
)

// ConstruirComandoWatermark superpone `wmPath` sobre `input` y guarda en `output`.
//
//	srcW     : ancho del video fuente (px) — para calcular el tamaño del watermark
//	wmPct    : tamaño del watermark como % del ancho del video
//	opacity  : 0.0–1.0
//	position : "top-left" | "top-right" | "bottom-left" | "bottom-right" | "center"
//	margin   : margen en px desde el borde (ignorado si position == "center")
func ConstruirComandoWatermark(input, output, wmPath string,
	srcW, wmPct int, opacity float64,
	position string, margin int) []string {

	if srcW <= 0 {
		srcW = 1920
	}
	if wmPct <= 0 || wmPct > 100 {
		wmPct = 15
	}
	if margin < 0 {
		margin = 0
	}
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}

	// Ancho objetivo del watermark. Par para evitar problemas de escalado.
	wmW := srcW * wmPct / 100
	if wmW < 16 {
		wmW = 16
	}
	if wmW%2 != 0 {
		wmW--
	}

	var xExpr, yExpr string
	switch position {
	case "top-left":
		xExpr = fmt.Sprintf("%d", margin)
		yExpr = fmt.Sprintf("%d", margin)
	case "top-right":
		xExpr = fmt.Sprintf("main_w-overlay_w-%d", margin)
		yExpr = fmt.Sprintf("%d", margin)
	case "bottom-left":
		xExpr = fmt.Sprintf("%d", margin)
		yExpr = fmt.Sprintf("main_h-overlay_h-%d", margin)
	case "center":
		xExpr = "(main_w-overlay_w)/2"
		yExpr = "(main_h-overlay_h)/2"
	default: // bottom-right
		xExpr = fmt.Sprintf("main_w-overlay_w-%d", margin)
		yExpr = fmt.Sprintf("main_h-overlay_h-%d", margin)
	}

	// [1:v] escala al ancho objetivo manteniendo ratio, fuerza RGBA
	// (necesario para que colorchannelmixer aplique opacidad a PNG sin alpha),
	// aplica opacidad y se superpone sobre [0:v] en la posición elegida.
	filter := fmt.Sprintf(
		"[1:v]scale=%d:-1,format=rgba,colorchannelmixer=aa=%.3f[wm];"+
			"[0:v][wm]overlay=%s:%s[outv]",
		wmW, opacity, xExpr, yExpr,
	)

	return []string{
		"-y",
		"-i", input,
		"-i", wmPath,
		"-filter_complex", filter,
		"-map", "[outv]",
		"-map", "0:a?", // audio si existe
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "copy", // los clips ya vienen en AAC
		"-movflags", "+faststart",
		output,
	}
}

// isAlreadyWatermarked detecta si el archivo ya tiene el sufijo _wm
// para no re-procesar en bucle al apuntar a una carpeta.
func isAlreadyWatermarked(path string) bool {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.HasSuffix(base, "_wm")
}

// doWatermark: handler del menú (opción 14).
func doWatermark(outDir string) bool {
	fmt.Println(ui.Col(ui.Cyan, "💧 Marca de agua a clips/videos"))
	fmt.Println()

	// 1) Imagen
	wmPath := ui.Ask("Ruta a la imagen de marca de agua (.png con transparencia recomendado)")
	if wmPath == "" || !ui.Exists(wmPath) {
		ui.ErrLn("No existe: " + wmPath)
		return false
	}
	ext := strings.ToLower(filepath.Ext(wmPath))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif":
	default:
		ui.WarnLn("Extensión poco común para watermark: " + ext)
	}

	// 2) Destino
	fmt.Println(ui.Col(ui.White, "¿Dónde aplicar la marca de agua?"))
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
			if !isAlreadyWatermarked(f) {
				files = append(files, f)
			}
		}
	default:
		raw, _ := filepath.Glob(filepath.Join(outDir, "clip_*.mp4"))
		for _, f := range raw {
			if !isAlreadyWatermarked(f) {
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		ui.ErrLn("No hay videos para procesar (¿ya tienen sufijo _wm?)")
		return false
	}

	// 3) Posición
	fmt.Println()
	fmt.Println(ui.Col(ui.White, "Posición:"))
	fmt.Println(ui.Col(ui.Gray, "  1) Arriba-izquierda"))
	fmt.Println(ui.Col(ui.Gray, "  2) Arriba-derecha"))
	fmt.Println(ui.Col(ui.Gray, "  3) Abajo-izquierda"))
	fmt.Println(ui.Col(ui.Gray, "  4) Abajo-derecha  (default)"))
	fmt.Println(ui.Col(ui.Gray, "  5) Centro"))
	posStr := ui.Ask("Elige 1-5 (vacío = 4)")
	position := "bottom-right"
	switch posStr {
	case "1":
		position = "top-left"
	case "2":
		position = "top-right"
	case "3":
		position = "bottom-left"
	case "5":
		position = "center"
	}

	// 4) Tamaño (% del ancho)
	pctStr := ui.Ask("Tamaño del watermark (% del ancho del video, vacío = 15)")
	wmPct := 15
	if pctStr != "" {
		if v, err := strconv.Atoi(pctStr); err == nil && v >= 1 && v <= 100 {
			wmPct = v
		}
	}

	// 5) Opacidad
	opStr := ui.Ask("Opacidad (0.0-1.0, vacío = 0.85)")
	opacity := 0.85
	if opStr != "" {
		if v, err := strconv.ParseFloat(opStr, 64); err == nil && v >= 0 && v <= 1 {
			opacity = v
		}
	}

	// 6) Margen
	mStr := ui.Ask("Margen en px (vacío = 40)")
	margin := 40
	if mStr != "" {
		if v, err := strconv.Atoi(mStr); err == nil && v >= 0 {
			margin = v
		}
	}

	// 7) Procesar
	fmt.Println()
	ok := 0
	for i, f := range files {
		srcW, _ := ObtenerDimensionesVideo(f)
		e := filepath.Ext(f)
		base := strings.TrimSuffix(f, e)
		output := base + "_wm" + e

		fmt.Printf("  [%d/%d] %s → %s\n",
			i+1, len(files), filepath.Base(f), filepath.Base(output))

		args := ConstruirComandoWatermark(f, output, wmPath,
			srcW, wmPct, opacity, position, margin)
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
	ui.OKLn(fmt.Sprintf("%d/%d videos con marca de agua", ok, len(files)))
	return true
}
