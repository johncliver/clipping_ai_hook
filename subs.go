package main

import (
	"fmt"
	"os"
	"strings"
)

const fuenteSubs = "Arial Black"

const assHeader = `[Script Info]
Title: Clips IA
ScriptType: v4.00+
PlayResX: 1080
PlayResY: 1920
WrapStyle: 0
ScaledBorderAndShadow: yes
YCbCr Matrix: TV.709

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: TikTok,%s,92,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,-1,0,0,0,100,100,1,0,1,7,3,2,60,60,420,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`

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
	fmt.Fprintf(&sb, assHeader, fuenteSubs)
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
