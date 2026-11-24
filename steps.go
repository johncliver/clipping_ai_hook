package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clipping_ia_hook/internal/ui"
)

// delayEntreChunks: espera FIJA tras completar cada chunk enviado a DeepSeek.
// No se mide desde el último envío: se duerme este tiempo completo después
// de cada chunk (excepto el último), independientemente de cuánto tardó
// DeepSeek en responder. Así el usuario ve el espaciado real y DeepSeek no
// recibe ráfagas.
const delayEntreChunks = 2 * time.Minute

func doTranscribe(source, outDir string) bool {
	ui.Ask("🎙️  Transcribiendo: " + source)
	t, err := ObtenerTranscripcion(source)
	if err != nil {
		ui.ErrLn("Transcripción: " + err.Error())
		return false
	}
	if t == nil || len(t.Segmentos) == 0 {
		ui.ErrLn("Transcripción vacía (0 segmentos). Revisa el audio o el idioma.")
		return false
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		ui.ErrLn("JSON: " + err.Error())
		return false
	}

	out := filepath.Join(outDir, "transcripcion.json")
	if err := os.WriteFile(out, data, 0644); err != nil {
		ui.ErrLn("Guardando " + out + ": " + err.Error())
		return false
	}

	abs, _ := filepath.Abs(out)
	ui.OKLn(fmt.Sprintf("%d segmentos → %s", len(t.Segmentos), abs))
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

// doPrompt genera prompt_deepseek.txt como REFERENCIA (prompt del video completo).
// En modo chunked doDeepSeek NO usará este archivo: generará un prompt por chunk.
func doPrompt(outDir string) bool {
	t, err := loadTranscripcionJSON(filepath.Join(outDir, "transcripcion.json"))
	if err != nil || t == nil || len(t.Segmentos) == 0 {
		ui.ErrLn("no hay transcripción válida")
		return false
	}

	fd, err := CargarCaras(filepath.Join(outDir, "caras.json"))
	if err != nil {
		ui.WarnLn("sin caras.json, el prompt irá sin caras")
		fd = nil
	}

	reglas, err := os.ReadFile("campaign.txt")
	if err != nil {
		ui.ErrLn("no se pudo leer campaign.txt: " + err.Error())
		return false
	}

	prompt := ConstruirPrompt(
		t.TextoTemporal(),
		string(reglas),
		fd,
		t.Duracion(),
		0,
		t.Duracion(),
	)

	if err := os.WriteFile(
		filepath.Join(outDir, "prompt_deepseek.txt"),
		[]byte(prompt), 0644); err != nil {
		ui.ErrLn("guardando prompt: " + err.Error())
		return false
	}

	ui.OKLn(fmt.Sprintf("prompt_deepseek.txt generado (%d chars)", len(prompt)))

	if n := len(ConstruirChunks(t, ChunkTamObjetivoSeg, ChunkOverlapSeg)); n > 1 {
		ui.WarnLn(fmt.Sprintf(
			"Video largo → DeepSeek se llamará en %d chunks. Este archivo es solo referencia.",
			n))
	}
	return true
}

// ══════════════════════════════════════════════════════════════
// doDeepSeek: orquesta single-shot o multi-chunk según duración.
// ══════════════════════════════════════════════════════════════
// doDeepSeek orquesta single-shot o multi-chunk con UNA SOLA sesión de Brave.
// El chat crece en el navegador: chunk 1 con prompt completo, chunks 2..N con
// prompt de continuación (más corto, lista clips previos, mismo chat).
func doDeepSeek(outDir string) bool {
	respPath := filepath.Join(outDir, "respuesta_deepseek.txt")
	transPath := filepath.Join(outDir, "transcripcion.json")
	carasPath := filepath.Join(outDir, "caras.json")

	if !ui.Exists(transPath) {
		ui.ErrLn("Falta transcripcion.json — corre el paso 1 primero")
		return false
	}
	if ui.Exists(respPath) {
		if !ui.AskYes("Ya existe " + respPath + ". ¿Regenerar?") {
			fmt.Println(ui.Col(ui.Gray, "  ↪ Reusando existente"))
			return true
		}
	}

	t, err := loadTranscripcionJSON(transPath)
	if err != nil || t == nil || len(t.Segmentos) == 0 {
		ui.ErrLn("transcripción inválida")
		return false
	}

	var fd *FaceData
	if ui.Exists(carasPath) {
		fd, _ = CargarCaras(carasPath)
	}
	if fd == nil {
		ui.WarnLn("sin caras.json (recomendado correr paso 2)")
	}

	reglas, err := os.ReadFile("campaign.txt")
	if err != nil {
		ui.ErrLn("no se pudo leer campaign.txt: " + err.Error())
		return false
	}

	fmt.Printf("📱 Plataforma: %s (máx %.0fs/clip)\n",
		plataformaActual.Nombre(), maxClipActual())

	chunks := ConstruirChunks(t, ChunkTamObjetivoSeg, ChunkOverlapSeg)
	if len(chunks) == 0 {
		ui.ErrLn("no se pudo construir chunks")
		return false
	}

	// ── Abrir UNA sola sesión (un solo chat) ──
	fmt.Println(ui.Col(ui.Cyan, "🤖 Abriendo Brave (una sola sesión para todos los chunks)..."))
	sesion, err := NuevaSesionDeepSeek()
	if err != nil {
		ui.ErrLn("abriendo Brave: " + err.Error())
		return false
	}
	defer sesion.Cerrar()

	if len(chunks) == 1 {
		fmt.Println(ui.Col(ui.Gray, "  ↪ una sola llamada (video corto)"))
		return doDeepSeekSingleConSesion(sesion, t, fd, string(reglas), outDir)
	}

	fmt.Printf("🎬 Video de %.1fs → %d chunks (~%.0fs c/u, overlap %.0fs, delay %.0fs)\n",
		t.Duracion(), len(chunks), ChunkTamObjetivoSeg, ChunkOverlapSeg,
		delayEntreChunks.Seconds())
	fmt.Println(ui.Col(ui.Gray,
		"   💾 Cada chunk se guarda. El chat es persistente: si cortas y relanzas,\n"+
			"      los chunks hechos se reusan y los pendientes continúan en un chat nuevo\n"+
			"      con la lista de clips previos para no duplicar."))

	var planes []*Plan

	for i, ch := range chunks {
		partialPath := filepath.Join(outDir,
			fmt.Sprintf("respuesta_deepseek_chunk_%02d.json", i+1))

		// Reusar si ya existe y parsea. No hay delay (no se envió nada).
		if ui.Exists(partialPath) {
			data, _ := os.ReadFile(partialPath)
			if plan, e := ParsearPlan(string(data)); e == nil {
				ui.OKLn(fmt.Sprintf("chunk %d/%d → reusando %s",
					i+1, len(chunks), filepath.Base(partialPath)))
				planes = append(planes, plan)
				continue
			}
			ui.WarnLn(fmt.Sprintf("chunk %d/%d existe pero no parsea, regenerando",
				i+1, len(chunks)))
		}

		fmt.Printf("\n🔄 chunk %d/%d [%.1fs-%.1fs, contexto %.1fs-%.1fs]\n",
			i+1, len(chunks), ch.InicioSeg, ch.FinSeg, ch.InicioCtx, ch.FinCtx)

		tChunk := recortarTranscripcion(t, ch.InicioCtx, ch.FinCtx)
		var fChunk *FaceData
		if fd != nil {
			fChunk = filtrarCaras(fd, ch.InicioCtx, ch.FinCtx)
		}

		// ── Prompt: completo para chunk 1, continuación para chunk 2..N ──
		var prompt string
		if i == 0 {
			prompt = ConstruirPrompt(
				tChunk.TextoTemporal(),
				string(reglas),
				fChunk,
				t.Duracion(),
				ch.InicioSeg,
				ch.FinSeg,
			)
			_ = os.WriteFile(
				filepath.Join(outDir, "prompt_deepseek_chunk_01.txt"),
				[]byte(ColapsarAUnaLinea(prompt)), 0644)
		} else {
			clipsPrevios := cargarClipsPreviosDeChunks(outDir)
			prompt = ConstruirPromptContinuacion(
				tChunk.TextoTemporal(),
				clipsPrevios,
				t.Duracion(),
				ch.InicioSeg,
				ch.FinSeg,
				i+1,
				len(chunks),
			)
			_ = os.WriteFile(
				filepath.Join(outDir, fmt.Sprintf("prompt_deepseek_chunk_%02d.txt", i+1)),
				[]byte(ColapsarAUnaLinea(prompt)), 0644)
		}

		promptOneLine := ColapsarAUnaLinea(prompt)
		fmt.Println(ui.Col(ui.Gray, fmt.Sprintf("  ↪ prompt %d chars", len(promptOneLine))))

		// ── Enviar al MISMO chat y esperar la nueva respuesta ──
		resp, e := sesion.Enviar(promptOneLine)

		if errors.Is(e, errRateLimit) {
			ui.ErrLn("⏸️  DeepSeek: 'Messages too frequent. Try again later.'")
			ui.WarnLn(fmt.Sprintf(
				"Chunks guardados: %d/%d. Relanza el paso 5 más tarde para reanudar.",
				i, len(chunks)))
			return false
		}

		if e != nil {
			ui.WarnLn(fmt.Sprintf("  ⚠️  chunk %d saltado: %v", i+1, e))
			// No reintentamos en el mismo chat: reenviar confunde al modelo.
			// Saltamos al siguiente chunk. El usuario puede relanzar paso 5.
		} else if plan, pe := ParsearPlan(resp); pe != nil {
			rawPath := partialPath + ".raw.txt"
			_ = os.WriteFile(rawPath, []byte(resp), 0644)
			ui.WarnLn(fmt.Sprintf(
				"  ⚠️  chunk %d: JSON inválido (%v). Respuesta cruda en %s",
				i+1, pe, filepath.Base(rawPath)))
			// No reintentamos: el JSON inválido ya quedó en el chat.
		} else {
			_ = os.WriteFile(partialPath, []byte(resp), 0644)
			ui.OKLn(fmt.Sprintf("  %d clips en chunk %d", len(plan.Clips), i+1))
			planes = append(planes, plan)
		}

		// ── Delay FIJO tras el chunk (solo si hay siguiente) ──
		if i < len(chunks)-1 {
			fmt.Printf("⏸️  esperando %s antes del siguiente chunk...\n",
				delayEntreChunks.Round(time.Second))
			time.Sleep(delayEntreChunks)
		}
	}

	if len(planes) == 0 {
		ui.ErrLn("ningún chunk produjo clips")
		return false
	}

	planFinal := FusionarPlanes(planes, t)
	data, err := json.MarshalIndent(planFinal, "", "  ")
	if err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	if err := os.WriteFile(respPath, data, 0644); err != nil {
		ui.ErrLn(err.Error())
		return false
	}
	ui.OKLn(fmt.Sprintf("%d clips finales tras fusión → %s",
		len(planFinal.Clips), respPath))
	return true
}

// doDeepSeekSingleConSesion: usa la sesión ya abierta (un solo envío).
func doDeepSeekSingleConSesion(
	sesion *SesionDeepSeek, t *Transcripcion, fd *FaceData,
	reglas, outDir string,
) bool {
	promptPath := filepath.Join(outDir, "prompt_deepseek.txt")
	onelinePath := filepath.Join(outDir, "prompt_deepseek_oneline.txt")
	respPath := filepath.Join(outDir, "respuesta_deepseek.txt")

	var prompt string
	if ui.Exists(onelinePath) {
		data, _ := os.ReadFile(onelinePath)
		prompt = strings.TrimSpace(string(data))
		fmt.Println(ui.Col(ui.Gray,
			fmt.Sprintf("  ↪ Usando one-line (%d chars)", len(prompt))))
	} else if ui.Exists(promptPath) {
		data, _ := os.ReadFile(promptPath)
		prompt = ColapsarAUnaLinea(string(data))
		_ = os.WriteFile(onelinePath, []byte(prompt), 0644)
		fmt.Println(ui.Col(ui.Gray,
			fmt.Sprintf("  ↪ Colapsado a one-line (%d chars)", len(prompt))))
	} else {
		p := ConstruirPrompt(t.TextoTemporal(), reglas, fd, t.Duracion(), 0, t.Duracion())
		prompt = ColapsarAUnaLinea(p)
		_ = os.WriteFile(promptPath, []byte(p), 0644)
		_ = os.WriteFile(onelinePath, []byte(prompt), 0644)
		fmt.Println(ui.Col(ui.Gray,
			fmt.Sprintf("  ↪ Prompt generado y colapsado (%d chars)", len(prompt))))
	}

	fmt.Println(ui.Col(ui.Cyan, "🤖 Consultando DeepSeek..."))
	resp, err := sesion.Enviar(prompt)
	if errors.Is(err, errRateLimit) {
		ui.ErrLn("⏸️  DeepSeek: 'Messages too frequent. Try again later.'")
		ui.WarnLn("Relanza el paso 5 más tarde.")
		return false
	}
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

// doDeepSeekSingle: una sola llamada. No aplica delay (no hay chunk siguiente).
func doDeepSeekSingle(t *Transcripcion, fd *FaceData, reglas, outDir string) bool {
	promptPath := filepath.Join(outDir, "prompt_deepseek.txt")
	onelinePath := filepath.Join(outDir, "prompt_deepseek_oneline.txt")
	respPath := filepath.Join(outDir, "respuesta_deepseek.txt")

	var prompt string
	if ui.Exists(onelinePath) {
		data, _ := os.ReadFile(onelinePath)
		prompt = strings.TrimSpace(string(data))
		fmt.Println(ui.Col(ui.Gray,
			fmt.Sprintf("  ↪ Usando one-line (%d chars)", len(prompt))))
	} else if ui.Exists(promptPath) {
		data, _ := os.ReadFile(promptPath)
		prompt = ColapsarAUnaLinea(string(data))
		_ = os.WriteFile(onelinePath, []byte(prompt), 0644)
		fmt.Println(ui.Col(ui.Gray,
			fmt.Sprintf("  ↪ Colapsado a one-line (%d chars)", len(prompt))))
	} else {
		p := ConstruirPrompt(t.TextoTemporal(), reglas, fd, t.Duracion(), 0, t.Duracion())
		prompt = ColapsarAUnaLinea(p)
		_ = os.WriteFile(promptPath, []byte(p), 0644)
		_ = os.WriteFile(onelinePath, []byte(prompt), 0644)
		fmt.Println(ui.Col(ui.Gray,
			fmt.Sprintf("  ↪ Prompt generado y colapsado (%d chars)", len(prompt))))
	}

	fmt.Println(ui.Col(ui.Cyan, "🤖 Consultando DeepSeek (abre Brave)..."))
	resp, err := ObtenerRespuestaDeepSeek(prompt)
	if errors.Is(err, errRateLimit) {
		ui.ErrLn("⏸️  DeepSeek: 'Messages too frequent. Try again later.'")
		ui.WarnLn("Relanza el paso 5 más tarde.")
		return false
	}
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
	fmt.Println(ui.Col(ui.Cyan, fmt.Sprintf(
		"  %d clips planificados · plataforma %s · crop texto: %s",
		len(plan.Clips), plataformaActual.Nombre(), ui.Estado(SeguirTexto))))
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
