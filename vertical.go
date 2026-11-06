package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"clipping_ia_hook/internal/ui"
)

// ConstruirComandoVertical arma el ffmpeg para convertir a 1080x1920.
//
//	modo = "crop" → recorta los lados (llena el frame, pierde ~68% del ancho)
//	modo = "blur" → fondo difuminado + video completo centrado (sin recortar)
func ConstruirComandoVertical(input, output string, srcW, srcH int, modo string) []string {
	targetW, targetH := 1080, 1920
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

// doVertical: handler del menú. Pide video, modo (crop/blur) y renderiza.
func doVertical(outDir string) bool {
	fmt.Println(ui.Col(ui.Cyan, "📱 Convertir video horizontal → vertical (1080x1920)"))
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
	fmt.Println(ui.Col(ui.Gray, fmt.Sprintf("  📐 Original: %dx%d → 1080x1920 (%s)", srcW, srcH, modo)))

	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	sufijo := "_vertical"
	if modo == "blur" {
		sufijo = "_vertical_blur"
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
