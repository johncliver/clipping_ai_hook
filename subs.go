package main

import (
	"clipping_ia_hook/internal/ui"
	"fmt"
	"os"
	"path/filepath"
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

// ══════════════════════════════════════════════════════════════
// Colores (formato ASS: &HAABBGGRR — alpha=00 opaco)
// ══════════════════════════════════════════════════════════════

var SubsColorBase = "&H00FFFFFF"    // texto normal (blanco)
var SubsColorKaraoke = "&H0000FFFF" // palabra activa (amarillo)

// Presets rápidos para el karaoke
var coloresKaraoke = []struct{ Nombre, ASS string }{
	{"Amarillo (default)", "&H0000FFFF"},
	{"Cyan", "&H00FFFF00"},
	{"Magenta", "&H00FF00FF"},
	{"Verde lima", "&H0000FF00"},
	{"Rojo", "&H000000FF"},
	{"Naranja", "&H000080FF"},
	{"Azul", "&H00FF0000"},
	{"Rosa", "&H00FF80FF"},
	{"Blanco", "&H00FFFFFF"},
}

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
Style: TikTok,%s,%d,%s,&H000000FF,&H00000000,&H80000000,-1,0,0,0,100,100,1,0,1,7,3,%d,60,60,%d,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`, fuenteSubs, SubsTamaño, SubsColorBase, alineacionASS(), SubsMargenV)
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
					linea.WriteString("{\\c" + SubsColorKaraoke + "&}")
					linea.WriteString(escaparASS(w.Texto))
					linea.WriteString("{\\c" + SubsColorBase + "&}")
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

// configurarColorKaraoke permite cambiar el color de la palabra activa.
// Solo afecta a los .ass generados DESPUÉS del cambio.
func configurarColorKaraoke() {
	fmt.Println()
	fmt.Println(ui.Col(ui.Cyan, "🎨 Color del karaoke"))
	fmt.Println(ui.Col(ui.Gray,
		"  Actual: karaoke="+SubsColorKaraoke+"   base="+SubsColorBase))
	fmt.Println()

	fmt.Println(ui.Col(ui.White, "Colores disponibles (palabra activa):"))
	for i, c := range coloresKaraoke {
		marca := " "
		if c.ASS == SubsColorKaraoke {
			marca = "●"
		}
		fmt.Printf("  %s %2d) %s\n", marca, i+1, c.Nombre)
	}
	op := ui.Ask("Elige (vacío = mantener)")
	if op == "" {
		return
	}
	v, err := strconv.Atoi(op)
	if err != nil || v < 1 || v > len(coloresKaraoke) {
		ui.ErrLn("Opción inválida")
		return
	}
	SubsColorKaraoke = coloresKaraoke[v-1].ASS
	ui.OKLn("Karaoke → " + coloresKaraoke[v-1].Nombre)
	fmt.Println(ui.Col(ui.Gray,
		"  ⚠️  Aplica solo a los .ass/clips que generes a partir de ahora"))
}

// ffmpegEscapeFilterPath prepara una ruta para usarla como valor de un
// filtro ffmpeg (p.ej. `ass=<ruta>`).
//
// Windows: una ruta como C:\Users\... tiene ":" que ffmpeg interpreta como
// separador de opciones del filtro DESPUÉS de un primer nivel de unescape
// del filtergraph. Por eso hay que escapar el ":" DOS veces (`\\:`).
//
// Como eso es frágil, primero intentamos devolver una ruta RELATIVA al cwd
// (sin "C:" ni escapes). Solo si no es posible usamos la absoluta escapada.
func ffmpegEscapeFilterPath(p string) string {
	// 1) Intentar relativa al cwd (sin ":" → sin escapes → siempre funciona)
	if abs, err := filepath.Abs(p); err == nil {
		if cwd, err := os.Getwd(); err == nil {
			if rel, err := filepath.Rel(cwd, abs); err == nil && !strings.Contains(rel, ":") {
				return filepath.ToSlash(rel)
			}
		}
	}
	// 2) Fallback: absoluta con doble escape de ":" (Windows / otra unidad)
	p = strings.ReplaceAll(p, `\`, `/`)
	p = strings.ReplaceAll(p, ":", `\\:`)
	return p
}

// doSubtitulosSolo: opción de menú independiente del pipeline.
// Toma transcripcion.json + el video fuente y devuelve el video COMPLETO
// con los subtítulos incrustados. No hace clips, no ejecuta el pipeline.
func doSubtitulosSolo(outDir string) {
	fmt.Println()
	fmt.Println(ui.Col(ui.Cyan, "🎬 Video con subtítulos incrustados"))
	fmt.Println(ui.Col(ui.Gray, "  Aplica el .ass sobre el video completo."))
	fmt.Println(ui.Col(ui.Gray, "  NO hace clips. NO ejecuta el pipeline."))
	fmt.Println()

	// 1) Transcripción existente
	transpPath := filepath.Join(outDir, "transcripcion.json")
	p := ui.Ask(fmt.Sprintf("Transcripción (vacío = %s)", transpPath))
	if p != "" {
		transpPath = p
	}
	t, err := loadTranscripcionJSON(transpPath)
	if err != nil {
		ui.ErrLn("Leyendo transcripción: " + err.Error())
		return
	}
	if len(t.Segmentos) == 0 {
		ui.ErrLn("La transcripción no tiene segmentos")
		return
	}

	// 2) Color karaoke (opcional)
	if ui.Ask("¿Cambiar color del karaoke? (s/N)") == "s" {
		configurarColorKaraoke()
	}

	// 3) Resolver el video fuente (local o descargar si es URL)
	videoPath, err := resolverVideoFuente(outDir)
	if err != nil {
		ui.ErrLn(err.Error())
		return
	}

	// 4) Generar .ass junto al video
	ext := filepath.Ext(videoPath)
	base := strings.TrimSuffix(videoPath, ext)
	assPath := base + ".subs.ass"
	if err := GenerarASS(t, assPath); err != nil {
		ui.ErrLn("Generando .ass: " + err.Error())
		return
	}
	ui.OKLn("Subtítulos generados → " + assPath)

	// 5) Quemar en el video completo
	outVideo := base + "_subs.mp4"
	absAss, _ := filepath.Abs(assPath)
	assFilter := "ass=" + ffmpegEscapeFilterPath(absAss)

	fmt.Println()
	fmt.Println(ui.Col(ui.Gray, "⏳ Quemando subtítulos (puede tardar según la duración)..."))

	args := []string{
		"-y",
		"-i", videoPath,
		"-vf", assFilter,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		outVideo,
	}
	if err := EjecutarFFmpeg(args); err != nil {
		ui.ErrLn("ffmpeg: " + err.Error())
		return
	}

	absOut, _ := filepath.Abs(outVideo)
	fmt.Println()
	ui.OKLn("✅ Video con subtítulos → " + absOut)
}

// resolverVideoFuente devuelve la ruta al video local para quemar los subs.
// Orden:
//  1. Si procesados/source.txt es un archivo local existente → lo usa.
//  2. Si es URL → ofrece descargarlo con yt-dlp a outDir/video.mp4.
//  3. Si no hay nada utilizable → pregunta ruta manual.
func resolverVideoFuente(outDir string) (string, error) {
	srcTxt := filepath.Join(outDir, "source.txt")
	if data, err := os.ReadFile(srcTxt); err == nil {
		candidate := strings.TrimSpace(string(data))
		if candidate != "" {
			// 1) Archivo local
			if ui.Exists(candidate) {
				fmt.Println(ui.Col(ui.Gray, "  📂 Video fuente: "+candidate))
				return candidate, nil
			}
			// 2) URL
			if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
				fmt.Println(ui.Col(ui.Gray, "  🔗 Origen es URL: "+candidate))
				if ui.Ask("¿Descargar el video ahora con yt-dlp? (s/N)") == "s" {
					fmt.Println(ui.Col(ui.Gray, "⏳ Descargando video..."))
					return descargarVideoEn(candidate, outDir)
				}
			}
		}
	}

	// 3) Preguntar manual
	v := ui.Ask("Ruta al video local")
	if v == "" || !ui.Exists(v) {
		return "", fmt.Errorf("video no encontrado: %s", v)
	}
	return v, nil
}
