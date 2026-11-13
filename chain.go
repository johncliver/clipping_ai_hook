package main

import (
	"fmt"
	"path/filepath"
	"strings"

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

// chain.go
func ensureVideo(source, outDir string) (string, error) {
	if esArchivoLocal(source) {
		return source, nil
	}

	// Reusar si ya existe (mp4 preferido; si hay mkv/webm también vale)
	if m, _ := filepath.Glob(filepath.Join(outDir, "video.*")); len(m) > 0 {
		for _, f := range m {
			switch strings.ToLower(filepath.Ext(f)) {
			case ".mp4", ".mkv", ".webm", ".mov":
				fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando "+f))
				return f, nil
			}
		}
	}

	fmt.Println(ui.Col(ui.Cyan, "📥 Descargando video de YouTube (H.264+AAC)…"))
	return descargarVideoEn(source, outDir)
}
