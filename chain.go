package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"clipping_ia_hook/internal/ui"
)

func chainFrom(step int, source, outDir string) {
	if step <= 4 {
		if !ensureTranscripcion(source, outDir) {
			ui.ErrLn("cadena abortada: no hay transcripción")
			return
		}
	}
	for s := step; s <= 6; s++ {
		ok := true
		switch s {
		case 3:
			if ui.Exists(filepath.Join(outDir, "texto.json")) {
				fmt.Println(ui.Col(ui.Gray, "  ↪ texto.json ya existe, saltando 3"))
				continue
			}
			fmt.Println(ui.Col(ui.Cyan, "  ▶ auto: paso 3 (texto en pantalla)"))
			ok = doText(source, outDir)
		case 4:
			if ui.Exists(filepath.Join(outDir, "prompt_deepseek.txt")) {
				fmt.Println(ui.Col(ui.Gray, "  ↪ prompt_deepseek.txt ya existe, saltando 4"))
				continue
			}
			fmt.Println(ui.Col(ui.Cyan, "  ▶ auto: paso 4 (prompt)"))
			ok = doPrompt(outDir)
		case 5:
			if ui.Exists(filepath.Join(outDir, "respuesta_deepseek.txt")) {
				fmt.Println(ui.Col(ui.Gray, "  ↪ respuesta_deepseek.txt ya existe, saltando 5"))
				continue
			}
			fmt.Println(ui.Col(ui.Cyan, "  ▶ auto: paso 5 (DeepSeek)"))
			ok = doDeepSeek(outDir)
		case 6:
			clips, _ := filepath.Glob(filepath.Join(outDir, "clip_*.mp4"))
			if len(clips) > 0 {
				fmt.Println(ui.Col(ui.Gray, fmt.Sprintf("  ↪ ya hay %d clips, saltando 6", len(clips))))
				continue
			}
			fmt.Println(ui.Col(ui.Cyan, "  ▶ auto: paso 6 (clips)"))
			ok = doClips(source, outDir)
		}
		if !ok {
			ui.ErrLn(fmt.Sprintf("cadena abortada en paso %d", s))
			return
		}
	}
	ui.OKLn("cadena completada")
}

func ensureTranscripcion(source, outDir string) bool {
	p := filepath.Join(outDir, "transcripcion.json")
	if ui.Exists(p) {
		return true
	}
	ui.WarnLn("falta transcripcion.json, generando (paso 1)...")
	return doTranscribe(source, outDir)
}

func ensureVideo(source, outDir string) (string, error) {
	if esArchivoLocal(source) {
		return source, nil
	}
	vp := filepath.Join(outDir, "video.mp4")
	if ui.Exists(vp) {
		fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando "+vp))
		return vp, nil
	}
	fmt.Println(ui.Col(ui.Cyan, "📥 Descargando video de YouTube → "+vp))
	cmd := exec.Command("yt-dlp",
		"-f", "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/best[height<=1080][ext=mp4]/best",
		"--merge-output-format", "mp4",
		"-o", vp, source,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("yt-dlp video: %w", err)
	}
	return vp, nil
}
