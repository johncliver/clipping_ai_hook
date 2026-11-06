package main

import (
	"fmt"
	"strings"
)

func ConstruirPrompt(transcripcion, reglasCampana string,
	faceData *FaceData, textData *TextData,
	duracion, inicio, fin float64) string {

	faceInfo := "Sin datos de caras disponibles."
	if faceData != nil && len(faceData.Samples) > 0 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Dimensiones: %dx%d. Muestreo (cx,cy relativos 0-1):\n",
			faceData.Width, faceData.Height)
		for _, s := range faceData.Samples {
			if len(s.Faces) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "t=%.1fs:", s.T)
			for _, f := range s.Faces {
				fmt.Fprintf(&sb, " [cx=%.2f,cy=%.2f]", f.CX, f.CY)
			}
			sb.WriteString("\n")
		}
		faceInfo = sb.String()
	}

	textInfo := "Sin texto en pantalla detectado."
	if textData != nil && len(textData.Samples) > 0 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Bloques de texto visibles (t, ancho relativo):\n")
		for _, s := range textData.Samples {
			for _, r := range s.Regions {
				if r.W < 0.15 {
					continue
				}
				fmt.Fprintf(&sb, "t=%.1fs w=%.2f\n", s.T, r.W)
			}
		}
		textInfo = sb.String()
	}

	// El bonus de paneo de lectura solo se ofrece si el crop persigue texto.
	bonusTexto := ""
	if SeguirTexto {
		bonusTexto = `- Bonus: si en el clip aparece texto en pantalla prominente (según los datos de
  arriba), la cámara hará un paneo de lectura automático sobre él. Puedes usar
  esto para elegir clips donde el texto refuerza el hook.
`
	}

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

PERFIL DE CARAS DETECTADAS (tiempos absolutos):
%s

TEXTO EN PANTALLA DETECTADO (títulos, carteles, captions ya incrustados en el video):
%s

MATERIAL: %.1f segundos en total (rango analizado: %.1fs - %.1fs del video original).

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
DURACIÓN Y CORTES (MUY IMPORTANTE — el usuario quiere clips MÁS LARGOS)
════════════════════════════════════════════════════════════════════
- APUNTA A CLIPS DE 30 A 90 SEGUNDOS. Ese es el rango objetivo.
- Solo baja de 20s cuando sea un one-liner DEMOLEDOR que se sostiene solo
  (una frase que no necesita nada más). Como regla general, evita < 20s.
- Tope duro por plataforma: %.0f segundos.

- NUNCA cortes una palabra a mitad. "fin_seg" DEBE caer sobre un final de
  frase natural: punto, coma larga, cambio de turno de habla, o pausa clara
  en la transcripción. Prefiere perder 2 segundos de contenido a cortar una
  palabra por la mitad.

- Añade "hold_final_seg" (0.3 a 1.5 segundos) para dejar RESPIRAR la última
  frase. No es silencio muerto: es un pequeño margen post-última-palabra para
  que el espectador procese lo que acaba de oír antes del corte. El pipeline
  lo respeta al renderizar. Usa 0.5-1.0s por defecto; sube a 1.2-1.5s si el
  clip termina en punchline o momento emotivo.

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
- "momento_entrada_seg": en qué segundo DEL CLIP entra la música.
  0.0 = desde el primer frame; >0 = después de la introducción hablada
  (ej. 3.0 = entra cuando Adam dice "Oh my God").
- "sugerencia": 1 línea describiendo el estilo, ej.:
    · "braam de impacto justo cuando el toro embiste"
    · "guitarra western ligera, tono cómico"
    · "piano emotivo para el momento padre-hijo"
    · "percusión de tensión creciente antes del overtake"
  Esta sugerencia la usará el editor humano para elegir la pista. Sé específico.

CUÁNDO PONER MÚSICA:
- "impacto" → golpes visuales, caídas, sorpresas, reacciones grandes.
- "tension" → antes de una acción (subir al toro, contar 3-2-1, esperar el
  veredicto). Baja intensidad, subiendo hacia el clímax.
- "comedia" → momentos absurdos, cultura shock, one-liners ridículos.
- "epico" → reveals, orgullo, "soy oficialmente un texano", team photo.
- "emotivo" → padre-hijo, "over the moon", primeras veces.
- "western" → todo el bloque Texas (boots, bull riding, barbecue, bats).
- "none" → si el clip es puro diálogo/reacción que se sostiene solo. No fuerces
  música cuando tapa el valor del audio original.

REGLA DE ORO DE MÚSICA: la música NUNCA debe tapar el diálogo principal. Si el
clip es una frase clave, usa "none" o intensidad "baja" con entrada tardía.

════════════════════════════════════════════════════════════════════
QUÉ SÍ QUIERES
════════════════════════════════════════════════════════════════════
- Reacciones reales, sorpresa, humor, tensión, frases citables, "Texas culture
  shock", one-liners de Dale, retos ridículos, vulnerabilidad, interacción con
  fans. Todo lo que huela a F1 Twitter, NO a anuncio.
- Clips que se entiendan sin haber visto el resto del video.
- Arcos completos: situación → desarrollo → reacción / cierre.
- Honestidad de contexto: respeta lo que realmente pasa en el video.

QUÉ NO QUIERES
════════════════════════════════════════════════════════════════════
- Relleno promocional, "compra Coinbase", discursos de marca, B-roll sin
  contexto.
- Clips que arranquen con 3 segundos de silencio o de "eh…".
- Frases cortadas a mitad que dejen al espectador con la duda.
- Clips que solo funcionan si has visto el resto del video.
- Clips de menos de 15s salvo que sean un one-liner demoledor.
- Cualquier cosa que infrinja las reglas de la campaña (aunque sea viral).

════════════════════════════════════════════════════════════════════
REGLAS DE TIEMPO
════════════════════════════════════════════════════════════════════
- "inicio_seg" y "fin_seg" son timestamps ABSOLUTOS del video original.
- inicio_seg < fin_seg siempre.
- fin_seg - inicio_seg <= %.0f (tope duro).
- fin_seg DEBE caer en un final de frase natural (prohibido cortar palabras).
- "hold_final_seg" entre 0.3 y 1.5 (margen de respiro después de la última
  palabra hablada). El render final alarga el clip ese tanto.
- No solapes clips en el tiempo: cada tramo pertenece a un solo clip.

════════════════════════════════════════════════════════════════════
CHECKLIST ANTES DE RESPONDER (hazlo mentalmente clip por clip)
════════════════════════════════════════════════════════════════════
1. ¿El clip cumple TODAS las reglas de la campaña (marcas, disclosure, claims)?
2. ¿La primera palabra del clip es la primera palabra del hook_texto?
3. ¿inicio_seg cae dentro de ±0.3s del inicio real del hook en la transcripción?
4. ¿fin_seg cae en un final de frase natural (sin cortar palabras)?
5. ¿El clip dura entre 20 y %.0f segundos? (ideal 30-90)
6. ¿El clip se entiende sin haber visto el resto del video?
7. ¿La música sugerida es coherente con el tono y NO tapa el diálogo?
8. ¿Los hashtags incluyen #ad o #partner y la mención a Coinbase?

Devuelve EXCLUSIVAMENTE un JSON válido (sin texto extra, sin markdown, solo JSON):
{
  "clips": [
    {
      "inicio_seg": 51.18,
      "fin_seg": 145.0,
      "hook_texto": "Genuinely, I am up for anything",
      "hold_final_seg": 0.8,
      "razon": "por qué este clip funciona viralmente (2-3 frases)",
      "hashtags": ["#ad", "#partner", "#Coinbase"],
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
- Los campos "hold_final_seg" y "musica" son NUEVOS: inclúyelos siempre.
- Ante la duda entre acortar o dejar respirar el clip, DEJA RESPIRAR.`,
		reglasCampana, transcripcion, faceInfo, textInfo,
		fin-inicio, inicio, fin,
		maxClipSegundos,
		bonusTexto,
		maxClipSegundos,
		maxClipSegundos)
}
