package main

import (
	"fmt"
	"strings"
)

// Tablas de variación para que el prompt de continuación NO sea idéntico
// en cada chunk. Rotan por índice de chunk.

var aperturasContinuacion = []string{
	"Perfecto. Continuemos con el resto del video.",
	"Bien. Ahora vamos con el siguiente tramo.",
	"Seguimos. Aquí tienes la siguiente sección.",
	"Vamos con el próximo bloque del video.",
	"Continuamos donde lo dejamos.",
	"Ahora el siguiente fragmento, sin repetir lo anterior.",
	"Sigamos con la siguiente parte.",
	"Bien. Pasemos al próximo tramo del video.",
}

var antiDuplicadoFrases = []string{
	"NO repitas ninguno de esos clips. Si un momento bueno ya está cubierto, ignóralo.",
	"No propongas clips que se solapen con los ya listados. Cada tramo pertenece a un solo clip.",
	"Evita duplicar: si ves un momento que ya está en la lista, sáltalo y busca otro.",
	"Sin duplicados. Si el hook ya aparece en los clips anteriores, no lo vuelvas a proponer.",
	"No repitas momentos ya propuestos. Prioriza material nuevo de esta ventana.",
}

var reglaExtraFrases = []string{
	"Prioriza reacciones reales de KreekCraft sobre momentos genéricos.",
	"Prefiere clips que se entiendan sin haber visto el resto del video.",
	"El logo overlay debe estar visible en TODO el clip (no lo olvides).",
	"El caption debe incluir 'Watch KreekCraft on YouTube and Twitch' sin links.",
	"Likes visibles obligatorios: si el clip no los muestra, será rechazado.",
	"Engagement mínimo 0.8%: elige momentos que generen interacción real.",
}

var cierresContinuacion = []string{
	"Devuelve el JSON puro, sin markdown ni texto extra.",
	"Respuesta: solo el JSON con los nuevos clips y advertencias.",
	"Mismo formato JSON que antes. Nada más.",
	"Escupe el JSON directamente, sin explicaciones.",
	"Solo JSON válido, como en las respuestas anteriores.",
}

// ConstruirPromptContinuacion arma el prompt que se inyecta en el MISMO chat
// para el chunk N (N>=2). Es más corto que el prompt del chunk 1 porque:
//   - No reenvía campaign.txt completo (ya está en el contexto del chat).
//   - Solo recuerda las reglas que más se olvidan.
//   - Lista los clips YA propuestos para evitar duplicados.
//
// clipsPrevios se lee de disco (respuesta_deepseek_chunk_*.json), así que
// incluso si el chat se reinicia, la continuación funciona.
func ConstruirPromptContinuacion(
	transcripcion string,
	clipsPrevios []ClipSpec,
	duracion, inicio, fin float64,
	numChunk, totalChunks int,
) string {

	ctxIni := inicio - ChunkOverlapSeg
	if ctxIni < 0 {
		ctxIni = 0
	}
	ctxFin := fin + ChunkOverlapSeg
	if ctxFin > duracion {
		ctxFin = duracion
	}

	idx := numChunk - 2 // chunk 2 → índice 0
	if idx < 0 {
		idx = 0
	}
	apertura := aperturasContinuacion[idx%len(aperturasContinuacion)]
	antiDup := antiDuplicadoFrases[idx%len(antiDuplicadoFrases)]
	extra := reglaExtraFrases[idx%len(reglaExtraFrases)]
	cierre := cierresContinuacion[idx%len(cierresContinuacion)]

	var sb strings.Builder

	sb.WriteString(apertura)
	sb.WriteString("\n\n")
	fmt.Fprintf(&sb, "Es el chunk %d de %d de este video.\n", numChunk, totalChunks)

	sb.WriteString("Ya propusiste los siguientes clips en esta conversación:\n")
	if len(clipsPrevios) == 0 {
		sb.WriteString("  (ninguno todavía)\n")
	} else {
		for i, c := range clipsPrevios {
			hook := c.HookTexto
			if hook == "" {
				hook = "(sin hook)"
			}
			fmt.Fprintf(&sb, "  %d. [%.1fs - %.1fs]  %q\n",
				i+1, c.InicioSeg, c.FinSeg, hook)
		}
	}
	sb.WriteString("\n")
	sb.WriteString(antiDup)
	sb.WriteString("\n\n")

	fmt.Fprintf(&sb,
		"TU NUEVA VENTANA: [%.2f, %.2f] (absolutos del video original)\n",
		inicio, fin)
	fmt.Fprintf(&sb,
		"El contexto de la transcripción de abajo cubre [%.2f, %.2f] "+
			"(±%.0fs extra para entender clips que cruzan bordes).\n\n",
		ctxIni, ctxFin, ChunkOverlapSeg)

	sb.WriteString("REGLAS CRÍTICAS (recuérdalas):\n")
	fmt.Fprintf(&sb, "- inicio_seg de cada clip DEBE caer en [%.2f, %.2f].\n", inicio, fin)
	sb.WriteString("- Máximo 5 clips en esta respuesta.\n")
	fmt.Fprintf(&sb, "- Tope duro por clip: %.0fs (ideal 30-%.0fs).\n",
		maxClipActual(), plataformaActual.IdealSeg())
	sb.WriteString("- Cada clip empieza EXACTAMENTE con su hook_texto (cita literal).\n")
	sb.WriteString("- fin_seg en final de frase natural, sin cortar palabras.\n")
	sb.WriteString("- Hashtags obligatorios: #GTA6 #KreekCraft #GrandTheftAutoVI + #ad #partner.\n")
	sb.WriteString("- Logo overlay visible TODO el clip, likes visibles, sin links en caption.\n")
	sb.WriteString("- ")
	sb.WriteString(extra)
	sb.WriteString("\n\n")

	fmt.Fprintf(&sb, "TRANSCRIPCIÓN DEL NUEVO TRAMO [%.1f, %.1f]:\n", ctxIni, ctxFin)
	sb.WriteString(transcripcion)
	sb.WriteString("\n\n")

	sb.WriteString(cierre)
	return sb.String()
}

// cargarClipsPreviosDeChunks lee todos los respuesta_deepseek_chunk_*.json
// que existan en outDir y devuelve sus ClipSpec concatenados y ordenados.
// Se usa para:
//   - Alimentar el prompt de continuación con los clips ya propuestos.
//   - Sobrevivir a reinicios: aunque el chat se cierre, el siguiente prompt
//     sabe qué clips se propusieron antes.
func cargarClipsPreviosDeChunks(outDir string) []ClipSpec {
	var out []ClipSpec
	pattern := strings.ReplaceAll(
		"respuesta_deepseek_chunk_*.json", "/", string('\\'))
	_ = pattern // (placeholder por si algún día se mueve a subdir)

	files := listarChunks(outDir)
	for _, f := range files {
		data, err := readFile(f)
		if err != nil {
			continue
		}
		plan, err := ParsearPlan(string(data))
		if err != nil {
			continue
		}
		out = append(out, plan.Clips...)
	}
	return out
}
