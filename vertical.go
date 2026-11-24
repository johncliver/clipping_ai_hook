package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"clipping_ia_hook/internal/ui"
)

// ══════════════════════════════════════════════════════════════
// Tamaño de SALIDA configurable (opción "Horizontal → Vertical")
// ══════════════════════════════════════════════════════════════
//
// targetWVertical × targetHVertical son las dimensiones finales que usará
// ConstruirComandoVertical. El modo crop/blur se adapta solo a ese aspect.
var (
	TamanoVerticalPreset = "9:16 (1080x1920)" // nombre legible para el menú
	targetWVertical      = 1080
	targetHVertical      = 1920
)

// presetTamanoVertical aplica un preset por nombre. false = no existe.
func presetTamanoVertical(nombre string) bool {
	switch nombre {
	case "9:16":
		targetWVertical, targetHVertical = 1080, 1920
		TamanoVerticalPreset = "9:16 (1080x1920)"
	case "9:16-small":
		targetWVertical, targetHVertical = 720, 1280
		TamanoVerticalPreset = "9:16 small (720x1280)"
	case "4:5":
		targetWVertical, targetHVertical = 1080, 1350
		TamanoVerticalPreset = "4:5 (1080x1350)"
	case "3:4":
		targetWVertical, targetHVertical = 1080, 1440
		TamanoVerticalPreset = "3:4 (1080x1440)"
	case "1:1":
		targetWVertical, targetHVertical = 1080, 1080
		TamanoVerticalPreset = "1:1 (1080x1080)"
	case "16:9":
		targetWVertical, targetHVertical = 1920, 1080
		TamanoVerticalPreset = "16:9 (1920x1080)"
	default:
		return false
	}
	return true
}

// configurarTamanoVertical: menú análogo a configurarCropHorizontal (opción 18).
// Solo afecta a la opción "Horizontal → Vertical".
func configurarTamanoVertical() {
	fmt.Println(ui.Col(ui.Cyan, "📐 Tamaño de salida (Horizontal → Vertical)"))
	fmt.Println()
	fmt.Println(ui.Col(ui.Gray,
		"  Afecta solo a la opción 'Horizontal → Vertical' (12)."))
	fmt.Println(ui.Col(ui.Gray,
		fmt.Sprintf("  Actual: %s (%dx%d)",
			TamanoVerticalPreset, targetWVertical, targetHVertical)))
	fmt.Println()

	fmt.Println(ui.Col(ui.White, "Presets:"))
	fmt.Println(ui.Col(ui.Gray, "  1) 9:16     1080x1920  (Reels/TikTok/Shorts — default)"))
	fmt.Println(ui.Col(ui.Gray, "  2) 9:16      720x1280  (mismo aspect, más liviano)"))
	fmt.Println(ui.Col(ui.Gray, "  3) 4:5      1080x1350  (feed Instagram)"))
	fmt.Println(ui.Col(ui.Gray, "  4) 3:4      1080x1440"))
	fmt.Println(ui.Col(ui.Gray, "  5) 1:1      1080x1080  (cuadrado)"))
	fmt.Println(ui.Col(ui.Gray, "  6) 16:9     1920x1080  (horizontal)"))
	fmt.Println(ui.Col(ui.Gray, "  7) custom   (ancho x alto a mano)"))
	op := ui.Ask("Elige 1-7 (vacío = no cambiar)")

	switch op {
	case "1":
		presetTamanoVertical("9:16")
	case "2":
		presetTamanoVertical("9:16-small")
	case "3":
		presetTamanoVertical("4:5")
	case "4":
		presetTamanoVertical("3:4")
	case "5":
		presetTamanoVertical("1:1")
	case "6":
		presetTamanoVertical("16:9")
	case "7":
		wStr := ui.Ask("Ancho en px (ej. 1080)")
		hStr := ui.Ask("Alto en px (ej. 1920)")
		w, errW := strconv.Atoi(wStr)
		h, errH := strconv.Atoi(hStr)
		if errW != nil || errH != nil || w < 128 || h < 128 || w > 4096 || h > 4096 {
			ui.ErrLn("Valores inválidos (enteros entre 128 y 4096)")
			return
		}
		// yuv420p exige dimensiones pares
		if w%2 != 0 {
			w--
		}
		if h%2 != 0 {
			h--
		}
		targetWVertical, targetHVertical = w, h
		TamanoVerticalPreset = fmt.Sprintf("custom %dx%d", w, h)
	case "":
		return
	default:
		ui.ErrLn("Opción inválida")
		return
	}

	ui.OKLn(fmt.Sprintf("Tamaño → %s (%dx%d)",
		TamanoVerticalPreset, targetWVertical, targetHVertical))
	ui.OKLn("Aplicado a las próximas conversiones Horizontal → Vertical")
}

// ══════════════════════════════════════════════════════════════
// Construcción del comando FFmpeg (usa el tamaño configurado)
// ══════════════════════════════════════════════════════════════
func ConstruirComandoVertical(input, output string, srcW, srcH int, modo string) []string {
	targetW, targetH := targetWVertical, targetHVertical
	if srcW <= 0 || srcH <= 0 {
		srcW, srcH = 1920, 1080
	}

	base := []string{"-y", "-i", input}

	if modo == "blur" {
		// Fondo: escala hasta llenar + crop → blur fuerte.
		// Frente: escala completa sin recortar → overlay centrado.
		fc := fmt.Sprintf(
			"[0:v]scale=%d:%d:force_original_aspect_ratio=increase,"+
				"crop=%d:%d,gblur=sigma=30[bg];"+
				"[0:v]scale=%d:%d:force_original_aspect_ratio=decrease[fg];"+
				"[bg][fg]overlay=(W-w)/2:(H-h)/2,format=yuv420p[v]",
			targetW, targetH, targetW, targetH,
			targetW, targetH,
		)
		base = append(base,
			"-filter_complex", fc,
			"-map", "[v]",
			"-map", "0:a?",
		)
	} else {
		// Crop centrado: mismo criterio que ConstruirComandoClip.
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
		vf := fmt.Sprintf("crop=%d:%d:x=%d:y=0,scale=%d:%d",
			cropW, cropH, cropX, targetW, targetH)
		base = append(base,
			"-vf", vf,
			"-map", "0:v:0",
			"-map", "0:a?",
		)
	}

	base = append(base,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		output,
	)
	return base
}

// doVertical: handler de la opción 12.
func doVertical(outDir string) bool {
	fmt.Println(ui.Col(ui.Cyan,
		fmt.Sprintf("📱 Convertir video horizontal → vertical (%dx%d)",
			targetWVertical, targetHVertical)))
	fmt.Println(ui.Col(ui.Gray,
		"  Tip: cambiá el tamaño desde el menú → 'Tamaño de salida (vertical)'"))
	fmt.Println()

	videoPath := ui.Ask("Ruta al video (.mp4/.mov/.mkv)")
	if videoPath == "" || !ui.Exists(videoPath) {
		ui.ErrLn("No existe: " + videoPath)
		return false
	}

	fmt.Println()
	fmt.Println(ui.Col(ui.White, "Modo de conversión:"))
	fmt.Println(ui.Col(ui.Gray, "  1) Recorte centrado   (llena el frame — ideal para Reels/TikTok/Shorts)"))
	fmt.Println(ui.Col(ui.Gray, "  2) Fondo difuminado   (video completo visible, sin recortar)"))
	modoStr := ui.Ask("Elige 1 o 2")
	modo := "crop"
	if modoStr == "2" {
		modo = "blur"
	}

	srcW, srcH := ObtenerDimensionesVideo(videoPath)
	fmt.Println(ui.Col(ui.Gray, fmt.Sprintf("  📐 Original: %dx%d → %dx%d (%s)",
		srcW, srcH, targetWVertical, targetHVertical, modo)))

	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	sufijo := fmt.Sprintf("_%dx%d", targetWVertical, targetHVertical)
	if modo == "blur" {
		sufijo += "_blur"
	}
	output := filepath.Join(outDir, base+sufijo+".mp4")

	fmt.Println(ui.Col(ui.Cyan, "🎬 Renderizando..."))
	args := ConstruirComandoVertical(videoPath, output, srcW, srcH, modo)
	if err := EjecutarFFmpeg(args); err != nil {
		ui.ErrLn("ffmpeg: " + err.Error())
		return false
	}
	ui.OKLn("Video listo: " + output)
	return true
}
