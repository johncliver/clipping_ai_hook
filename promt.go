package main

import (
	"fmt"
	"strings"
)

// resumirCaras (sin cambios)
func resumirCaras(fd *FaceData) string {
	if fd == nil || len(fd.Samples) == 0 {
		return "Sin datos de caras disponibles."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Video %dx%d, fps=%.2f, duración=%.1fs.\n",
		fd.Width, fd.Height, fd.FPS, fd.Duration)
	sb.WriteString("Cómo leer: cada turno indica un tramo donde el tracker visual de boca\n")
	sb.WriteString("detectó a alguien hablando. cx,cy = centro de la cara (0-1 relativo al frame).\n")
	sb.WriteString("mouth = energía media de movimiento de boca (más alto = más habla).\n")
	sb.WriteString("Los id son tracks, NO identidades estables: no asumas que id=5 en el min 1\n")
	sb.WriteString("y id=5 en el min 5 son la misma persona.\n\n")

	type turno struct {
		t0, t1   float64
		id       int
		mouthSum float64
		mouthN   int
		cxSum    float64
		cySum    float64
		posN     int
	}
	var turnos []turno
	var cur *turno

	cerrar := func() {
		if cur != nil {
			turnos = append(turnos, *cur)
			cur = nil
		}
	}

	for _, s := range fd.Samples {
		spID := -1
		if s.SpeakingID != nil {
			spID = *s.SpeakingID
		}
		var f *FaceDetect
		if spID >= 0 {
			for i := range s.Faces {
				if s.Faces[i].ID == spID {
					f = &s.Faces[i]
					break
				}
			}
		}
		var ref *FaceDetect = f
		if ref == nil && len(s.Faces) > 0 {
			ref = &s.Faces[0]
			for i := range s.Faces {
				if s.Faces[i].W > ref.W {
					ref = &s.Faces[i]
				}
			}
		}

		continuar := false
		if cur != nil && spID >= 0 && cur.id == spID && s.T-cur.t1 <= 1.5 {
			continuar = true
		}
		if !continuar {
			cerrar()
			if spID >= 0 {
				cur = &turno{t0: s.T, t1: s.T, id: spID}
			}
		}
		if cur != nil {
			cur.t1 = s.T
			if f != nil {
				cur.mouthSum += f.MouthMotion
				cur.mouthN++
			}
			if ref != nil {
				cur.cxSum += ref.CX
				cur.cySum += ref.CY
				cur.posN++
			}
		}
	}
	cerrar()

	sb.WriteString("TURNOS DE HABLA (visual, por movimiento de boca):\n")
	if len(turnos) == 0 {
		sb.WriteString("(no se detectaron turnos claros de habla)\n")
	}
	for _, tn := range turnos {
		mouth := 0.0
		if tn.mouthN > 0 {
			mouth = tn.mouthSum / float64(tn.mouthN)
		}
		cx, cy := 0.5, 0.5
		if tn.posN > 0 {
			cx = tn.cxSum / float64(tn.posN)
			cy = tn.cySum / float64(tn.posN)
		}
		fmt.Fprintf(&sb, "[%.1f-%.1f] mouth≈%.1f | cx=%.2f cy=%.2f\n",
			tn.t0, tn.t1, mouth, cx, cy)
	}

	sb.WriteString("\nTRAMOS SIN HABLA DETECTADA (>=2s, buenos para silencios/música/reacciones):\n")
	var gaps []string
	prevFin := 0.0
	for _, tn := range turnos {
		if tn.t0-prevFin >= 2.0 {
			gaps = append(gaps, fmt.Sprintf("%.1f-%.1f", prevFin, tn.t0))
		}
		prevFin = tn.t1
	}
	if fd.Duration-prevFin >= 2.0 {
		gaps = append(gaps, fmt.Sprintf("%.1f-%.1f", prevFin, fd.Duration))
	}
	if len(gaps) == 0 {
		sb.WriteString("(ninguno)\n")
	} else {
		sb.WriteString(strings.Join(gaps, ", "))
		sb.WriteString("\n")
	}

	sb.WriteString("\nTRAMOS SIN CARA VISIBLE (>=2s, el crop 9:16 quedaría en negro/borroso):\n")
	var sinCara []string
	runStart := -1.0
	runEnd := -1.0
	flushSinCara := func() {
		if runStart >= 0 && runEnd-runStart >= 2.0 {
			sinCara = append(sinCara, fmt.Sprintf("%.1f-%.1f", runStart, runEnd))
		}
		runStart = -1
	}
	for _, s := range fd.Samples {
		if len(s.Faces) == 0 {
			if runStart < 0 {
				runStart = s.T
			}
			runEnd = s.T
		} else {
			flushSinCara()
		}
	}
	flushSinCara()
	if len(sinCara) == 0 {
		sb.WriteString("(ninguno)\n")
	} else {
		sb.WriteString(strings.Join(sinCara, ", "))
		sb.WriteString("\n")
	}

	return sb.String()
}

// ConstruirPrompt: si el rango [inicio, fin] NO cubre el video completo,
// se asume modo chunk y se añade la REGLA DE VENTANA. Usa la plataforma
// actual para el tope duro y el rango "ideal".
func ConstruirPrompt(transcripcion, reglasCampana string,
	faceData *FaceData,
	duracion, inicio, fin float64) string {

	faceInfo := resumirCaras(faceData)

	esChunk := inicio > 0.5 || fin < duracion-0.5

	var encabezadoTiempos string
	if esChunk {
		ctxIni := inicio - ChunkOverlapSeg
		if ctxIni < 0 {
			ctxIni = 0
		}
		ctxFin := fin + ChunkOverlapSeg
		if ctxFin > duracion {
			ctxFin = duracion
		}
		encabezadoTiempos = fmt.Sprintf(
			`DURACIÓN TOTAL DEL VIDEO: %.1f segundos.

⚠️  ESTA LLAMADA CUBRE UN CHUNK, NO EL VIDEO COMPLETO.
TU VENTANA DE TRABAJO: [%.2f, %.2f] (timestamps absolutos del video original).
La transcripción y los datos de caras de arriba cubren [%.2f, %.2f]
(±%.0fs de contexto extra a cada lado para que entiendas cómo acaba un
clip que empieza cerca de un borde).

REGLA DE VENTANA (CRÍTICA, NO LA ROMPAS):
- inicio_seg de CADA clip debe estar dentro de [%.2f, %.2f].
- Si ves un momento bueno que empieza FUERA de tu ventana, IGNÓRALO.
  Otro chunk lo verá y lo propondrá. Duplicar no suma.
- Un clip SÍ puede terminar fuera de tu ventana (con el contexto extra
  puedes decidir cómo acaba).

Devuelve COMO MÁXIMO %d clips.`,
			duracion, inicio, fin, ctxIni, ctxFin, ChunkOverlapSeg,
			inicio, fin, MaxClipsPorChunk)
	} else {
		encabezadoTiempos = fmt.Sprintf(
			"DURACIÓN TOTAL DEL VIDEO: %.1f segundos.", duracion)
	}

	maxClip := maxClipActual()
	ideal := plataformaActual.IdealSeg()

	return fmt.Sprintf(`Eres un editor viral de élite para TikTok, Instagram Reels y YouTube Shorts.
Tu trabajo es sacar de este material TODOS los clips que merezcan la pena, con criterio de
alguien que vive en F1 Twitter / foros de fans. Nada de sonar a anuncio corporativo.

════════════════════════════════════════════════════════════════════
PRIORIDAD #1: LAS REGLAS DE LA CAMPAÑA ESTÁN POR ENCIMA DE TODO.
Si una idea viral viola las reglas de la campaña (marcas, disclosure, claims
prohibidos, tono, música no licenciada), DESCÁRTALA. Antes de proponer un clip,
verifica que cumple TODAS las reglas. Si dudas, busca otro momento.
════════════════════════════════════════════════════════════════════

REGLAS DE LA CAMPAÑA (obligatorias, no las rompas):
%s

TRANSCRIPCIÓN CON TIMESTAMPS (tiempos absolutos del video original):
%s

HABLA VISUAL Y POSICIÓN DE CARAS (tiempos absolutos):
%s

%s

════════════════════════════════════════════════════════════════════
CÓMO USAR LOS DATOS DE CARAS
════════════════════════════════════════════════════════════════════
- Los "TURNOS DE HABLA" son detección visual (movimiento de boca), NO diarización
  perfecta. Úsalos para saber DÓNDE hay alguien hablando y DÓNDE mirar (cx).
- Los "TRAMOS SIN HABLA" son buenos para: silencios que se pueden cortar,
  reacciones sin diálogo, momentos donde entra música.
- Los "TRAMOS SIN CARA" son tramos donde un crop 9:16 automático quedaría en
  negro/borroso: evítalos o úsalos solo si el audio es excepcional.
- Si un clip cruza varios turnos de habla, es señal de conversación real
  (bueno para clips largos 30-90s).
- cx es el centro horizontal de la cara. El crop 9:16 se centra ahí. Puedes
  sugerir crop_center_x cerca de la cx del hablante principal del clip.

════════════════════════════════════════════════════════════════════
REGLA DEL HOOK (OBLIGATORIA, NO NEGOCIABLE)
════════════════════════════════════════════════════════════════════
- Cada clip DEBE empezar EXACTAMENTE con el hook. La primera palabra que se
  escuche en el clip debe ser la primera palabra del "hook_texto".
- "inicio_seg" DEBE coincidir (±0.3s) con el momento de la transcripción en que
  empieza el "hook_texto".
- PROHIBIDO poner "setup" o contexto ANTES del hook. El clip arranca en el hook.
- "hook_texto" DEBE ser una cita LITERAL de la transcripción (palabras exactas).
- Si el hook es una presentación ("Well, I'm Dale Brisby…", "Welcome to Texas…"),
  una pregunta, una exclamación o una afirmación fuerte, YA ES válido como
  primera línea. No hace falta descartarlo por "necesitar contexto": esas frases
  SON el contexto.
- Únicamente descarta un momento si su primera frase NO se entiende ni como
  presentación ni como reacción. En ese caso busca otro punto.

════════════════════════════════════════════════════════════════════
DURACIÓN Y CORTES (MUY IMPORTANTE)
════════════════════════════════════════════════════════════════════
- APUNTA A CLIPS DE 30 A %.0f SEGUNDOS. Ese es el rango objetivo.
- Solo baja de 20s cuando sea un one-liner DEMOLEDOR que se sostiene solo.
  Como regla general, evita < 20s.
- Tope duro por plataforma: %.0f segundos.

- NUNCA cortes una palabra a mitad. "fin_seg" DEBE caer sobre un final de
  frase natural: punto, coma larga, cambio de turno de habla, o pausa clara
  en la transcripción. Prefiere perder 2 segundos de contenido a cortar una
  palabra por la mitad.

- Añade "hold_final_seg" (0.3 a 1.5 segundos) para dejar RESPIRAR la última
  frase. El pipeline lo respeta al renderizar. Usa 0.5-1.0s por defecto;
  sube a 1.2-1.5s si el clip termina en punchline o momento emotivo.

- El clip debe CUBRIR UN ARCO COMPLETO: empieza donde arranca la acción o la
  frase que se va a desarrollar, y termina donde la idea se resuelve. Nada de
  cortar en el clímax sin la reacción.

- FLEXIBILIDAD TOTAL DE INICIO Y FIN: puedes elegir CUALQUIER par de
  timestamps válidos del video (principio, medio, final). No tienen que ser
  consecutivos ni alineados a segmentos. El único requisito es respetar la
  regla del hook.

════════════════════════════════════════════════════════════════════
MÚSICA DE FONDO (sugerida según contexto)
════════════════════════════════════════════════════════════════════
Para cada clip, propón un objeto "musica" con:
- "tipo": "impacto" | "tension" | "comedia" | "epico" | "emotivo" | "western" | "none"
- "intensidad": "baja" | "media" | "alta"
- "momento_entrada_seg": segundo DEL CLIP en que entra la música.
  0.0 = desde el primer frame; >0 = después de la introducción hablada.
- "sugerencia": 1 línea describiendo el estilo, sé específico.

CUÁNDO PONER MÚSICA:
- "impacto" → golpes visuales, caídas, sorpresas, reacciones grandes.
- "tension" → antes de una acción. Baja intensidad, subiendo hacia el clímax.
- "comedia" → momentos absurdos, cultura shock, one-liners ridículos.
- "epico" → reveals, orgullo, team photo.
- "emotivo" → padre-hijo, primeras veces.
- "western" → todo el bloque Texas.
- "none" → si el clip es puro diálogo/reacción que se sostiene solo.

REGLA DE ORO DE MÚSICA: la música NUNCA debe tapar el diálogo principal.

════════════════════════════════════════════════════════════════════
QUÉ SÍ QUIERES
════════════════════════════════════════════════════════════════════
- Reacciones reales, sorpresa, humor, tensión, frases citables, cultura shock,
  one-liners, retos ridículos, vulnerabilidad, interacción con fans.
- Clips que se entiendan sin haber visto el resto del video.
- Arcos completos: situación → desarrollo → reacción / cierre.

QUÉ NO QUIERES
════════════════════════════════════════════════════════════════════
- Relleno promocional, discursos de marca, B-roll sin contexto.
- Clips que arranquen con 3 segundos de silencio o de "eh…".
- Frases cortadas a mitad que dejen al espectador con la duda.
- Clips que solo funcionan si has visto el resto del video.
- Clips de menos de 15s salvo que sean un one-liner demoledor.
- Cualquier cosa que infrinja las reglas de la campaña.

════════════════════════════════════════════════════════════════════
REGLAS DE TIEMPO
════════════════════════════════════════════════════════════════════
- "inicio_seg" y "fin_seg" son timestamps ABSOLUTOS del video original.
- inicio_seg < fin_seg siempre.
- fin_seg - inicio_seg <= %.0f (tope duro).
- fin_seg DEBE caer en un final de frase natural (prohibido cortar palabras).
- "hold_final_seg" entre 0.3 y 1.5.
- No solapes clips en el tiempo: cada tramo pertenece a un solo clip.

════════════════════════════════════════════════════════════════════
CHECKLIST ANTES DE RESPONDER (hazlo mentalmente clip por clip)
════════════════════════════════════════════════════════════════════
1. ¿El clip cumple TODAS las reglas de la campaña (marcas, disclosure, claims)?
2. ¿La primera palabra del clip es la primera palabra del hook_texto?
3. ¿inicio_seg cae dentro de ±0.3s del inicio real del hook en la transcripción?
4. ¿fin_seg cae en un final de frase natural (sin cortar palabras)?
5. ¿El clip dura entre 20 y %.0f segundos? (ideal 30-%.0f)
6. ¿El clip se entiende sin haber visto el resto del video?
7. ¿La música sugerida es coherente con el tono y NO tapa el diálogo?
8. ¿Los hashtags incluyen #ad o #partner y la mención a la marca?

Devuelve EXCLUSIVAMENTE un JSON válido (sin texto extra, sin markdown, solo JSON):
{
  "clips": [
    {
      "inicio_seg": 51.18,
      "fin_seg": 145.0,
      "hook_texto": "Genuinely, I am up for anything",
      "hold_final_seg": 0.8,
      "razon": "por qué este clip funciona viralmente (2-3 frases)",
      "hashtags": ["#ad", "#partner"],
      "crop_center_x": 0.45,
      "musica": {
        "tipo": "comedia",
        "intensidad": "media",
        "momento_entrada_seg": 1.5,
        "sugerencia": "guitarra western ligera y cómica, entra cuando aparece el rancho"
      }
    }
  ],
  "advertencias": ["riesgos de incumplimiento detectados, si los hay; vacío si no hay"]
}

NOTAS FINALES:
- Devuelve el JSON puro, sin texto antes ni después, sin bloques de markdown.
- Si algún campo no aplica (ej. música none), usa "none" / "baja" / 0.0 y sigue.
- Ante la duda entre acortar o dejar respirar el clip, DEJA RESPIRAR.`,
		reglasCampana, transcripcion, faceInfo,
		encabezadoTiempos,
		ideal,
		maxClip,
		maxClip,
		maxClip,
		ideal)
}
