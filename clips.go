package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Tope duro de duración por clip (2:50).
const maxClipSegundos = 170.0

// Lead-in antes del hook (para no cortar el primer sonido).
const hookLeadIn = 0.10

// Tolerancia de alineación del inicio_seg con el hook.
const hookTolerancia = 0.30

// --- Paneo de lectura ---

// Velocidad del paneo cuando la cámara "lee" texto (px de video fuente / s).
const paneoLecturaPxPerSec = 520.0

// Ancho mínimo (relativo) de un bloque de texto para que active paneo de lectura.
const textoMinAnchoRel = 0.15

// Si el texto ocupa más que esto del frame, prioridad sobre las caras.
const textoDominaAnchoRel = 0.35

// SeguirTexto controla si el crop persigue texto en pantalla.
// Desactivado por defecto: el crop solo sigue caras.
// Se puede alternar desde el menú (opción 10).
var SeguirTexto = false

type ClipSpec struct {
	InicioSeg   float64  `json:"inicio_seg"`
	FinSeg      float64  `json:"fin_seg"`
	HookTexto   string   `json:"hook_texto"`
	Razon       string   `json:"razon"`
	Hashtags    []string `json:"hashtags"`
	CropCenterX float64  `json:"crop_center_x"`
	FFmpegCmd   string   `json:"ffmpeg_command,omitempty"`
}

type Plan struct {
	Clips        []ClipSpec `json:"clips"`
	Advertencias []string   `json:"advertencias"`
}

// ---- Normalización ----

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func normalizarPalabra(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Trim(s, ".,!?;:\"'()[]{}-")
}

func normalizarPalabras(s string) []string {
	var out []string
	for _, f := range strings.Fields(s) {
		n := normalizarPalabra(f)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func terminaFrase(s string) bool {
	s = strings.TrimRight(s, " \t\n\"')]")
	if s == "" {
		return false
	}
	return strings.ContainsRune(".!?;:", rune(s[len(s)-1]))
}

// ---- Hook anchoring ----

func ajustarInicioAHook(clip *ClipSpec, t *Transcripcion) {
	if clip.HookTexto == "" {
		return
	}
	palabras := aplanarPalabras(t)
	if len(palabras) == 0 {
		return
	}
	hookWords := normalizarPalabras(clip.HookTexto)
	if len(hookWords) == 0 {
		return
	}
	searchLen := 3
	if len(hookWords) < searchLen {
		searchLen = len(hookWords)
	}
	needle := hookWords[:searchLen]

	mejorStart := -1.0
	mejorDist := 1e18
	for i := 0; i+len(needle) <= len(palabras); i++ {
		match := true
		for j, hw := range needle {
			if normalizarPalabra(palabras[i+j].Texto) != hw {
				match = false
				break
			}
		}
		if match {
			hookStart := palabras[i].Inicio
			dist := absFloat(hookStart - clip.InicioSeg)
			if dist < mejorDist {
				mejorDist = dist
				mejorStart = hookStart
			}
		}
	}
	if mejorStart < 0 {
		return
	}
	if mejorDist <= hookTolerancia {
		return
	}
	nuevo := mejorStart - hookLeadIn
	if nuevo < 0 {
		nuevo = 0
	}
	clip.InicioSeg = nuevo
}

// ---- Fin natural ----

func extenderFinNatural(clip *ClipSpec, t *Transcripcion, maxExtra float64) {
	palabras := aplanarPalabras(t)
	if len(palabras) == 0 {
		return
	}
	if maxExtra < 0 {
		maxExtra = 0
	}
	limite := clip.FinSeg + maxExtra
	original := clip.FinSeg

	idx := -1
	for i, p := range palabras {
		if p.Fin >= clip.FinSeg-0.05 {
			idx = i
			break
		}
	}
	if idx < 0 {
		ultimo := palabras[len(palabras)-1].Fin
		if ultimo > clip.FinSeg {
			clip.FinSeg = ultimo
		}
		return
	}
	for i := idx; i < len(palabras); i++ {
		p := palabras[i]
		if p.Fin > limite {
			if i > 0 && palabras[i-1].Fin > original {
				clip.FinSeg = palabras[i-1].Fin
			}
			return
		}
		if terminaFrase(p.Texto) {
			if p.Fin > original {
				clip.FinSeg = p.Fin
			}
			return
		}
	}
	for i := len(palabras) - 1; i >= 0; i-- {
		if palabras[i].Fin <= limite && palabras[i].Fin > original {
			clip.FinSeg = palabras[i].Fin
			return
		}
	}
}

func mejorFinDeFraseAntes(t *Transcripcion, maxFin, inicio float64) float64 {
	palabras := aplanarPalabras(t)
	mejor := maxFin
	for _, p := range palabras {
		if p.Fin > maxFin {
			break
		}
		if p.Fin <= inicio+2 {
			continue
		}
		if terminaFrase(p.Texto) {
			mejor = p.Fin
		}
	}
	return mejor
}

// ---- Validación completa ----

func validarClip(clip *ClipSpec, t *Transcripcion) {
	if t == nil || len(t.Segmentos) == 0 {
		return
	}
	durTotal := t.Duracion()
	if durTotal <= 0 {
		return
	}
	ajustarInicioAHook(clip, t)
	if clip.InicioSeg < 0 {
		clip.InicioSeg = 0
	}
	if clip.InicioSeg >= durTotal-0.1 {
		clip.InicioSeg = durTotal - 0.1
	}
	if clip.FinSeg > durTotal {
		clip.FinSeg = durTotal
	}
	if clip.FinSeg <= clip.InicioSeg {
		clip.FinSeg = clip.InicioSeg + 1
	}
	maxFin := clip.InicioSeg + maxClipSegundos
	if maxFin > durTotal {
		maxFin = durTotal
	}
	if clip.FinSeg > maxFin {
		clip.FinSeg = mejorFinDeFraseAntes(t, maxFin, clip.InicioSeg)
		if clip.FinSeg <= clip.InicioSeg+0.5 {
			clip.FinSeg = maxFin
		}
	}
	limiteExtra := maxFin - clip.FinSeg
	if limiteExtra < 0 {
		limiteExtra = 0
	}
	extenderFinNatural(clip, t, limiteExtra)
	if clip.FinSeg > maxFin {
		clip.FinSeg = maxFin
	}
	if clip.FinSeg <= clip.InicioSeg {
		clip.FinSeg = clip.InicioSeg + 1
	}
}

// ---- Crop track unificado (caras + texto opcional) ----

// regionProminente devuelve la región de texto más ancha en un instante dado,
// o nil si ninguna supera textoMinAnchoRel.
func regionProminente(td *TextData, tAbs float64) *TextRegion {
	if td == nil {
		return nil
	}
	// Ventana de ±0.4s para tolerar variaciones de muestreo
	const win = 0.4
	var best *TextRegion
	for i := range td.Samples {
		s := &td.Samples[i]
		if s.T < tAbs-win || s.T > tAbs+win {
			continue
		}
		for j := range s.Regions {
			r := &s.Regions[j]
			if r.W < textoMinAnchoRel {
				continue
			}
			if best == nil || r.W > best.W {
				best = r
			}
		}
	}
	return best
}

// facePosicionEn devuelve la x objetivo (px) de la cara relevante en un instante,
// o -1 si no hay cara.
func facePosicionEn(fd *FaceData, tAbs float64, cropW, srcW int) float64 {
	if fd == nil {
		return -1
	}
	const win = 0.4
	var best *FaceDetect
	for i := range fd.Samples {
		s := fd.Samples[i]
		if s.T < tAbs-win || s.T > tAbs+win {
			continue
		}
		if face := elegirCara(s); face != nil {
			if best == nil || face.W > best.W {
				best = face
			}
		}
	}
	if best == nil {
		return -1
	}
	x := best.CX*float64(srcW) - float64(cropW)/2
	maxX := float64(srcW - cropW)
	if x < 0 {
		x = 0
	}
	if x > maxX {
		x = maxX
	}
	return x
}

// construirCropTrackUnificado mezcla caras y (opcionalmente) texto en una serie
// temporal y devuelve keyframes comprimidos para FFmpeg.
//
// Reglas:
//   - Si SeguirTexto está OFF (por defecto): el crop solo sigue caras.
//   - Si SeguirTexto está ON y hay texto dominante (>textoDominaAnchoRel):
//     gana el texto. Si el bloque es más ancho que el crop, se panea a
//     velocidad de lectura.
//   - Si no hay ninguno, mantiene la última posición.
func construirCropTrackUnificado(fd *FaceData, td *TextData,
	inicio, fin float64, cropW, srcW int) []CropKey {

	maxX := float64(srcW - cropW)
	if maxX < 0 {
		maxX = 0
	}

	const dt = 0.4 // paso de muestreo interno
	n := int((fin-inicio)/dt) + 1
	if n < 1 {
		n = 1
	}

	// Posición inicial
	x := maxX / 2.0
	if fd != nil {
		if fx := facePosicionEn(fd, inicio, cropW, srcW); fx >= 0 {
			x = fx
		}
	}

	type sample struct{ t, x float64 }
	var series []sample

	// Para paneo de lectura dentro de un bloque:
	var lectT0 float64 = -1 // inicio del bloque actual de texto dominante
	var lectX0, lectX1 float64
	var lectDur float64

	for i := 0; i < n; i++ {
		tAbs := inicio + float64(i)*dt
		var target float64 = -1

		// El texto SOLO influye en el crop si el usuario lo activa.
		var r *TextRegion
		if SeguirTexto {
			r = regionProminente(td, tAbs)
		}

		if r != nil {
			if r.W >= textoDominaAnchoRel {
				// Texto manda. Calcular rango de paneo dentro del bloque.
				textoXPx := r.X * float64(srcW)
				textoWPx := r.W * float64(srcW)
				if textoWPx <= float64(cropW) {
					// Cabe: centrar sobre el texto
					target = textoXPx + textoWPx/2 - float64(cropW)/2
					lectT0 = -1
				} else {
					// No cabe: panear a velocidad de lectura
					lectX0 = textoXPx - 20
					lectX1 = textoXPx + textoWPx - float64(cropW) + 20
					lectDur = (lectX1 - lectX0) / paneoLecturaPxPerSec
					if lectDur < 0.4 {
						lectDur = 0.4
					}
					if lectT0 < 0 || absFloat(tAbs-lectT0) > 4.0 {
						lectT0 = tAbs
					}
					frac := (tAbs - lectT0) / lectDur
					if frac > 1 {
						frac = 1
					}
					target = lectX0 + (lectX1-lectX0)*frac
				}
			} else {
				// Texto presente pero no dominante: se ignora para el crop
				lectT0 = -1
			}
		} else {
			lectT0 = -1
		}

		if target < 0 {
			// Sin texto dominante (o SeguirTexto=false): cara
			if fx := facePosicionEn(fd, tAbs, cropW, srcW); fx >= 0 {
				target = fx
			} else {
				target = x
			}
		}

		if target < 0 {
			target = 0
		}
		if target > maxX {
			target = maxX
		}
		x = target
		series = append(series, sample{t: tAbs - inicio, x: x})
	}

	// Comprimir a keyframes significativos
	const umbralPx = 55.0
	const minInterval = 1.1
	keys := []CropKey{{T: series[0].t, XPx: int(series[0].x)}}
	prevX := series[0].x
	lastT := series[0].t
	for _, s := range series[1:] {
		if absFloat(s.x-prevX) > umbralPx && (s.t-lastT) > minInterval {
			keys = append(keys, CropKey{T: s.t, XPx: int(s.x)})
			prevX = s.x
			lastT = s.t
		}
	}
	const maxKeys = 25
	if len(keys) > maxKeys {
		keys = keys[:maxKeys]
	}
	return keys
}

// ---- Plan y ejecución ----

func ParsearPlan(respuestaIA string) (*Plan, error) {
	jsonStr := extraerJSON(respuestaIA)
	var plan Plan
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("JSON inválido: %w", err)
	}
	if len(plan.Clips) == 0 {
		return nil, fmt.Errorf("sin clips en respuesta")
	}
	return &plan, nil
}

func extraerJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return s[start : end+1]
}

func GenerarClip(videoPath string, t *Transcripcion,
	faceData *FaceData, textData *TextData,
	clip ClipSpec, idx int, outDir string) error {

	if clip.FinSeg <= clip.InicioSeg {
		return fmt.Errorf("clip %d con tiempos inválidos", idx)
	}

	assPath := filepath.Join(outDir, fmt.Sprintf("clip_%02d.ass", idx))
	if err := GenerarASSClip(t, assPath, clip.InicioSeg, clip.FinSeg); err != nil {
		fmt.Printf("   ⚠️  sin subtítulos para clip %d: %v\n", idx, err)
		assPath = ""
	}

	output := filepath.Join(outDir, fmt.Sprintf("clip_%02d.mp4", idx))

	if clip.FFmpegCmd != "" && strings.HasPrefix(strings.TrimSpace(clip.FFmpegCmd), "ffmpeg") {
		cmdStr := clip.FFmpegCmd
		cmdStr = strings.ReplaceAll(cmdStr, "{input}", videoPath)
		cmdStr = strings.ReplaceAll(cmdStr, "{output}", output)
		cmdStr = strings.ReplaceAll(cmdStr, "{ass}", filepathToSlash(assPath))
		args := shellSplit(cmdStr)
		if len(args) > 1 && args[0] == "ffmpeg" {
			if err := EjecutarFFmpeg(args[1:]); err == nil {
				fmt.Printf("   ✅ clip %d (comando IA): %s\n", idx+1, output)
				return nil
			}
			fmt.Printf("   ⚠️  comando de DeepSeek falló, usando el generado\n")
		}
	}

	srcW, srcH := 1920, 1080
	if faceData != nil && faceData.Width > 0 {
		srcW, srcH = faceData.Width, faceData.Height
	}

	cropX := clip.CropCenterX
	if cropX <= 0 || cropX >= 1 {
		cropX = 0.5
	}

	args := ConstruirComandoClip(videoPath, output, assPath,
		clip.InicioSeg, clip.FinSeg, cropX, faceData, textData, srcW, srcH)
	if err := EjecutarFFmpeg(args); err != nil {
		return fmt.Errorf("ffmpeg clip %d: %w", idx, err)
	}
	fmt.Printf("   ✅ clip %d: %s\n", idx+1, output)
	return nil
}

func shellSplit(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	var quoteChar rune
	for _, r := range s {
		switch {
		case inQuote:
			if r == quoteChar {
				inQuote = false
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			inQuote = true
			quoteChar = r
		case r == ' ' || r == '\t' || r == '\n':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
