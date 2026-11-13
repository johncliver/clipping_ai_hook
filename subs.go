package main

import (
	"clipping_ia_hook/internal/ui"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const fuenteSubs = "Arial Black"

// ══════════════════════════════════════════════════════════════
// Posición de subtítulos (configurable desde el menú)
// ══════════════════════════════════════════════════════════════

var SubsPosicion = "bottom" // "bottom" | "center" | "top"
var SubsMargenV = 420       // margen vertical en px
var SubsTamaño = 92         // tamaño de fuente en pt

// Alineaciones ASS: 1-3 abajo (L,C,R), 4-6 centro, 7-9 arriba (L,C,R)
const (
	alinBottom = 2 // bottom-center
	alinCenter = 5 // middle-center
	alinTop    = 8 // top-center
)

func alineacionASS() int {
	switch SubsPosicion {
	case "top":
		return alinTop
	case "center":
		return alinCenter
	default:
		return alinBottom
	}
}

func generarASSHeader() string {
	return fmt.Sprintf(`[Script Info]
Title: Clips IA
ScriptType: v4.00+
PlayResX: 1080
PlayResY: 1920
WrapStyle: 0
ScaledBorderAndShadow: yes
YCbCr Matrix: TV.709

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: TikTok,%s,%d,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,-1,0,0,0,100,100,1,0,1,7,3,%d,60,60,%d,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`, fuenteSubs, SubsTamaño, alineacionASS(), SubsMargenV)
}

func GenerarASS(t *Transcripcion, salida string) error {
	palabras := aplanarPalabras(t)
	if len(palabras) == 0 {
		return fmt.Errorf("no hay palabras")
	}
	return renderASS(palabras, salida)
}

// GenerarASSClip genera un .ass con tiempos relativos al clip [inicio, fin].
func GenerarASSClip(t *Transcripcion, salida string, inicio, fin float64) error {
	palabras := aplanarPalabras(t)
	dur := fin - inicio
	var filtradas []Palabra
	for _, p := range palabras {
		if p.Fin <= inicio || p.Inicio >= fin {
			continue
		}
		ri := p.Inicio - inicio
		rf := p.Fin - inicio
		if ri < 0 {
			ri = 0
		}
		if rf > dur {
			rf = dur
		}
		if rf <= ri {
			continue
		}
		filtradas = append(filtradas, Palabra{Texto: p.Texto, Inicio: ri, Fin: rf})
	}
	if len(filtradas) == 0 {
		return fmt.Errorf("sin palabras en [%.1f-%.1f]", inicio, fin)
	}
	return renderASS(filtradas, salida)
}

func renderASS(palabras []Palabra, salida string) error {
	var sb strings.Builder
	sb.WriteString(generarASSHeader())
	const maxPalabrasChunk = 3
	for i := 0; i < len(palabras); {
		start := i
		end := i
		for end < len(palabras) && end-start < maxPalabrasChunk {
			end++
			if end-1 >= 0 && strings.ContainsAny(palabras[end-1].Texto, ".,!?;:") {
				break
			}
		}
		chunk := palabras[start:end]
		if len(chunk) == 0 {
			i++
			continue
		}
		i = end
		for j, activa := range chunk {
			var linea strings.Builder
			for k, w := range chunk {
				if k > 0 {
					linea.WriteByte(' ')
				}
				if k == j {
					linea.WriteString(`{\c&H0000FFFF&}`)
					linea.WriteString(escaparASS(w.Texto))
					linea.WriteString(`{\c&H00FFFFFF&}`)
				} else {
					linea.WriteString(escaparASS(w.Texto))
				}
			}
			fmt.Fprintf(&sb, "Dialogue: 0,%s,%s,TikTok,,0,0,0,,%s\n",
				assTime(activa.Inicio), assTime(activa.Fin), linea.String())
		}
	}
	return os.WriteFile(salida, []byte(sb.String()), 0644)
}

func aplanarPalabras(t *Transcripcion) []Palabra {
	var out []Palabra
	for _, seg := range t.Segmentos {
		if len(seg.Palabras) > 0 {
			for _, p := range seg.Palabras {
				p.Texto = strings.TrimSpace(p.Texto)
				if p.Texto != "" && p.Fin > p.Inicio {
					out = append(out, p)
				}
			}
			continue
		}
		pal := strings.Fields(seg.Texto)
		if len(pal) == 0 {
			continue
		}
		paso := (seg.Fin - seg.Inicio) / float64(len(pal))
		for i, p := range pal {
			out = append(out, Palabra{Texto: p, Inicio: seg.Inicio + float64(i)*paso, Fin: seg.Inicio + float64(i+1)*paso})
		}
	}
	return out
}

func assTime(seg float64) string {
	if seg < 0 {
		seg = 0
	}
	cs := int(seg * 100)
	h := cs / 360000
	m := (cs / 6000) % 60
	s := (cs / 100) % 60
	c := cs % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, c)
}

func escaparASS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "{", `\{`)
	s = strings.ReplaceAll(s, "}", `\}`)
	s = strings.ReplaceAll(s, "\n", `\N`)
	return s
}

// configurarSubtitulos permite cambiar posición, margen y tamaño.
// Solo afecta a los .ass / clips que se generen DESPUÉS del cambio.
func configurarSubtitulos() {
	fmt.Println(ui.Col(ui.Cyan, "📝 Posición de subtítulos"))
	fmt.Println()

	icono := map[string]string{
		"bottom": "⬇️  Abajo", "center": "⏺️  Centro", "top": "⬆️  Arriba",
	}[SubsPosicion]
	fmt.Println(ui.Col(ui.Gray, fmt.Sprintf(
		"  Actual: %s · margen %dpx · fuente %dpt",
		icono, SubsMargenV, SubsTamaño)))
	fmt.Println()

	fmt.Println(ui.Col(ui.White, "Posición:"))
	fmt.Println(ui.Col(ui.Gray, "  1) Abajo   (default)"))
	fmt.Println(ui.Col(ui.Gray, "  2) Centro"))
	fmt.Println(ui.Col(ui.Gray, "  3) Arriba"))
	op := ui.Ask("Elige 1-3 (vacío = no cambiar)")

	switch op {
	case "1":
		SubsPosicion = "bottom"
		SubsMargenV = 420
	case "2":
		SubsPosicion = "center"
		SubsMargenV = 0
	case "3":
		SubsPosicion = "top"
		SubsMargenV = 420
	case "":
		// mantener
	default:
		ui.ErrLn("Opción inválida")
		return
	}
	if op == "1" || op == "2" || op == "3" {
		ui.OKLn(fmt.Sprintf("Posición → %s (margen %dpx)", SubsPosicion, SubsMargenV))
	}

	mStr := ui.Ask(fmt.Sprintf("Margen vertical en px (vacío = %d)", SubsMargenV))
	if mStr != "" {
		if v, err := strconv.Atoi(mStr); err == nil && v >= 0 {
			SubsMargenV = v
			ui.OKLn(fmt.Sprintf("Margen vertical → %dpx", SubsMargenV))
		}
	}

	tStr := ui.Ask(fmt.Sprintf("Tamaño de fuente en pt (vacío = %d)", SubsTamaño))
	if tStr != "" {
		if v, err := strconv.Atoi(tStr); err == nil && v >= 20 && v <= 200 {
			SubsTamaño = v
			ui.OKLn(fmt.Sprintf("Fuente → %dpt", SubsTamaño))
		}
	}

	ui.OKLn("Aplicado a los próximos clips que generes")
	fmt.Println(ui.Col(ui.Gray,
		"  ⚠️  Los clips ya existentes conservan la posición anterior (subtítulos ya incrustados)"))
}
