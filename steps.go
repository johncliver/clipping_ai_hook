package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clipping_ia_hook/internal/ui"
)

func doTranscribe(source, outDir string) bool {
	path := filepath.Join(outDir, "transcripcion.json")
	if ui.Exists(path) {
		if !ui.AskYes("Ya existe " + path + ". ¿Regenerar?") {
			fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando existente"))
			return true
		}
	}
	fmt.Println(ui.Col(ui.Cyan, "🎬 Transcribiendo..."))
	t, err := ObtenerTranscripcion(source)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	data, _ := json.MarshalIndent(t, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	ui.OKLn(fmt.Sprintf("%d segmentos → %s", len(t.Segmentos), path))
	return true
}

func doFaces(source, outDir string) bool {
	path := filepath.Join(outDir, "caras.json")
	if ui.Exists(path) {
		if !ui.AskYes("Ya existe " + path + ". ¿Regenerar?") {
			fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando existente"))
			return true
		}
	}
	videoPath, err := ensureVideo(source, outDir)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	fmt.Println(ui.Col(ui.Cyan, "👤 Detectando caras (puede tardar)..."))
	fd, err := DetectarCaras(videoPath)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	if err := GuardarCaras(fd, path); err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	ui.OKLn(fmt.Sprintf("%d muestras → %s", len(fd.Samples), path))
	return true
}

func doText(source, outDir string) bool {
	path := filepath.Join(outDir, "texto.json")
	if ui.Exists(path) {
		if !ui.AskYes("Ya existe " + path + ". ¿Regenerar?") {
			fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando existente"))
			return true
		}
	}
	videoPath, err := ensureVideo(source, outDir)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	fmt.Println(ui.Col(ui.Cyan, "📝 Detectando texto en pantalla (puede tardar)..."))
	td, err := DetectarTexto(videoPath)
	if err != nil {
		ui.WarnLn(err.Error() + " — continuando sin texto")
		return true
	}
	if err := GuardarTexto(td, path); err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	ui.OKLn(fmt.Sprintf("%d muestras con texto → %s", len(td.Samples), path))
	return true
}

func doPrompt(outDir string) bool {
	transPath := filepath.Join(outDir, "transcripcion.json")
	if !ui.Exists(transPath) && ui.Exists("transcripcion.json") {
		transPath = "transcripcion.json"
	}
	if !ui.Exists(transPath) {
		ui.ErrLn("Falta transcripcion.json (paso 1)")
		return false
	}
	promptPath := filepath.Join(outDir, "prompt_deepseek.txt")
	if ui.Exists(promptPath) {
		if !ui.AskYes("Ya existe " + promptPath + ". ¿Regenerar?") {
			fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando existente"))
			return true
		}
	}
	t, err := loadTranscripcionJSON(transPath)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	var fd *FaceData
	if ui.Exists(filepath.Join(outDir, "caras.json")) {
		fd, _ = CargarCaras(filepath.Join(outDir, "caras.json"))
	}
	var td *TextData
	if ui.Exists(filepath.Join(outDir, "texto.json")) {
		td, _ = CargarTexto(filepath.Join(outDir, "texto.json"))
	}
	rules, err := os.ReadFile("campaign.txt")
	if err != nil {
		ui.ErrLn("No pude leer campaign.txt: " + err.Error())
		return false
	}

	startStr := ui.Ask("Inicio del análisis (HH:MM:SS o seg, vacío = 0)")
	if startStr == "" {
		startStr = "0"
	}
	endStr := ui.Ask("Fin del análisis (vacío = total)")
	inicio := parseTS(startStr, 0)
	fin := parseTS(endStr, t.Duracion())

	tPrompt := t
	if inicio > 0 || fin < t.Duracion() {
		fmt.Println(ui.Col(ui.Cyan, fmt.Sprintf("  ↪ Filtrando [%.1fs - %.1fs]", inicio, fin)))
		tPrompt = recortarTranscripcion(t, inicio, fin)
		if fd != nil {
			fd = filtrarCaras(fd, inicio, fin)
		}
		if td != nil {
			td = filtrarTexto(td, inicio, fin)
		}
	}

	prompt := ConstruirPrompt(tPrompt.TextoTemporal(), string(rules), fd, td, fin-inicio, inicio, fin)
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		ui.ErrLn(err.Error())
		return false
	}

	ui.OKLn(fmt.Sprintf("Prompt → %s (%d chars)", promptPath, len(prompt)))

	// NUEVO: versión colapsada a una línea (para DeepSeek sin problemas de textarea)
	onelinePath := filepath.Join(outDir, "prompt_deepseek_oneline.txt")
	oneline := ColapsarAUnaLinea(prompt)
	if err := os.WriteFile(onelinePath, []byte(oneline), 0644); err != nil {
		ui.WarnLn("guardando one-line: " + err.Error())
	} else {
		ui.OKLn(fmt.Sprintf("One-line → %s (%d chars)", onelinePath, len(oneline)))
	}
	return true
}

func doDeepSeek(outDir string) bool {
	promptPath := filepath.Join(outDir, "prompt_deepseek.txt")
	onelinePath := filepath.Join(outDir, "prompt_deepseek_oneline.txt")
	respPath := filepath.Join(outDir, "respuesta_deepseek.txt")

	if !ui.Exists(promptPath) {
		ui.WarnLn("Falta prompt_deepseek.txt — generando (paso 4)...")
		if !doPrompt(outDir) {
			return false
		}
	}
	if ui.Exists(respPath) {
		if !ui.AskYes("Ya existe " + respPath + ". ¿Regenerar?") {
			fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando existente"))
			return true
		}
	}

	// Preferir la versión de UNA LÍNEA (mucho más robusta al inyectar)
	var prompt string
	if ui.Exists(onelinePath) {
		data, _ := os.ReadFile(onelinePath)
		prompt = strings.TrimSpace(string(data))
		fmt.Println(ui.Col(ui.Gray,
			fmt.Sprintf("  ↪ Usando one-line (%d chars)", len(prompt))))
	} else {
		data, _ := os.ReadFile(promptPath)
		prompt = ColapsarAUnaLinea(string(data))
		// Persistir para la próxima vez
		_ = os.WriteFile(onelinePath, []byte(prompt), 0644)
		fmt.Println(ui.Col(ui.Gray,
			fmt.Sprintf("  ↪ Colapsado a one-line (%d chars)", len(prompt))))
	}

	fmt.Println(ui.Col(ui.Cyan, "🤖 Consultando DeepSeek (abre Brave)..."))
	resp, err := ObtenerRespuestaDeepSeek(prompt)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	if err := os.WriteFile(respPath, []byte(resp), 0644); err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	ui.OKLn(fmt.Sprintf("Respuesta → %s (%d chars)", respPath, len(resp)))
	return true
}

func doClips(source, outDir string) bool {
	transPath := filepath.Join(outDir, "transcripcion.json")
	if !ui.Exists(transPath) && ui.Exists("transcripcion.json") {
		transPath = "transcripcion.json"
	}
	if !ui.Exists(transPath) {
		ui.ErrLn("Falta transcripcion.json (paso 1)")
		return false
	}
	respPath := filepath.Join(outDir, "respuesta_deepseek.txt")
	if !ui.Exists(respPath) {
		ui.ErrLn("Falta " + respPath + " (paso 5)")
		return false
	}
	t, err := loadTranscripcionJSON(transPath)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	var fd *FaceData
	if ui.Exists(filepath.Join(outDir, "caras.json")) {
		fd, _ = CargarCaras(filepath.Join(outDir, "caras.json"))
	}
	var td *TextData
	if ui.Exists(filepath.Join(outDir, "texto.json")) {
		td, _ = CargarTexto(filepath.Join(outDir, "texto.json"))
	}
	videoPath, err := ensureVideo(source, outDir)
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	resp, _ := os.ReadFile(respPath)
	plan, err := ParsearPlan(string(resp))
	if err != nil {
		ui.ErrLn(err.Error() + " (revisa " + respPath + ")")
		return false
	}
	fmt.Println(ui.Col(ui.Cyan, fmt.Sprintf("  %d clips planificados (crop texto: %s)",
		len(plan.Clips), ui.Estado(SeguirTexto))))
	for i, clip := range plan.Clips {
		validarClip(&clip, t)
		fmt.Println(ui.Col(ui.White, fmt.Sprintf(
			"  → Clip %d: [%.1fs - %.1fs] %s",
			i+1, clip.InicioSeg, clip.FinSeg, ui.Col(ui.Gray, clip.HookTexto))))
		if err := GenerarClip(videoPath, t, fd, td, clip, i, outDir); err != nil {
			ui.WarnLn(err.Error())
		}
	}
	ui.OKLn("Clips en " + outDir + "/")
	if len(plan.Advertencias) > 0 {
		ui.WarnLn("Advertencias de DeepSeek:")
		for _, a := range plan.Advertencias {
			fmt.Println(ui.Col(ui.Yellow, "     - "+a))
		}
	}
	return true
}
