package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Lead-in antes del hook (para no cortar el primer sonido).
const hookLeadIn = 0.10

// Tolerancia de alineación del inicio_seg con el hook.
const hookTolerancia = 0.30

// --- Paneo de lectura ---

const paneoLecturaPxPerSec = 520.0
const textoMinAnchoRel = 0.15
const textoDominaAnchoRel = 0.35

var SeguirTexto = false

// ══════════════════════════════════════════════════════════════
// Plataforma de destino (afecta al tope de duración por clip)
// ══════════════════════════════════════════════════════════════

type Plataforma int

const (
	PlataformaYouTube Plataforma = iota // Shorts: máx 120s
	PlataformaReels                     // TikTok/Reels/Shorts general: máx 170s
	PlataformaLarga                     // Plataformas que aceptan clips largos: máx 300s
)

var plataformaActual = PlataformaYouTube

func (p Plataforma) Nombre() string {
	switch p {
	case PlataformaYouTube:
		return "YouTube Shorts (≤2:00)"
	case PlataformaReels:
		return "TikTok/Reels (≤2:50)"
	case PlataformaLarga:
		return "Larga (≤5:00)"
	}
	return "?"
}

func (p Plataforma) MaxSeg() float64 {
	switch p {
	case PlataformaYouTube:
		return 120.0
	case PlataformaReels:
		return 170.0
	case PlataformaLarga:
		return 300.0
	}
	return 170.0
}

// IdealSeg: extremo superior del rango "ideal" sugerido al modelo.
func (p Plataforma) IdealSeg() float64 {
	switch p {
	case PlataformaYouTube:
		return 90.0
	case PlataformaReels:
		return 120.0
	case PlataformaLarga:
		return 180.0
	}
	return 90.0
}

func maxClipActual() float64 { return plataformaActual.MaxSeg() }

// ══════════════════════════════════════════════════════════════

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

// ---- Validación completa (usa maxClipActual()) ----

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
	maxSeg := maxClipActual()
	maxFin := clip.InicioSeg + maxSeg
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

// ---- Crop track unificado (sin cambios de fondo, solo imports) ----

func regionProminente(td *TextData, tAbs float64) *TextRegion {
	if td == nil {
		return nil
	}
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

func hayHablaEnVentana(t *Transcripcion, t0, t1 float64) bool {
	if t == nil {
		return false
	}
	for _, seg := range t.Segmentos {
		if seg.Fin <= t0 {
			continue
		}
		if seg.Inicio >= t1 {
			return false
		}
		return true
	}
	return false
}

func buscarCaraHablante(fd *FaceData, t0, t1 float64) *FaceDetect {
	if fd == nil {
		return nil
	}
	var best *FaceDetect
	bestMotion := -1.0
	for i := range fd.Samples {
		s := &fd.Samples[i]
		if s.T < t0 || s.T > t1 {
			continue
		}
		if s.SpeakingID != nil {
			for j := range s.Faces {
				if s.Faces[j].ID == *s.SpeakingID {
					if s.Faces[j].MouthMotion > bestMotion {
						bestMotion = s.Faces[j].MouthMotion
						best = &s.Faces[j]
					}
				}
			}
		}
	}
	if best != nil {
		return best
	}
	for i := range fd.Samples {
		s := &fd.Samples[i]
		if s.T < t0 || s.T > t1 {
			continue
		}
		for j := range s.Faces {
			if best == nil || s.Faces[j].W > best.W {
				best = &s.Faces[j]
			}
		}
	}
	return best
}

func facePosicionConHabla(fd *FaceData, t *Transcripcion, tAbs float64,
	cropW, srcW int) float64 {
	if fd == nil {
		return -1
	}
	maxX := float64(srcW - cropW)
	if maxX < 0 {
		maxX = 0
	}
	clampX := func(x float64) float64 {
		if x < 0 {
			return 0
		}
		if x > maxX {
			return maxX
		}
		return x
	}
	if hayHablaEnVentana(t, tAbs, tAbs+1.2) {
		if face := buscarCaraHablante(fd, tAbs, tAbs+1.2); face != nil {
			return clampX(face.CX*float64(srcW) - float64(cropW)/2)
		}
	}
	if hayHablaEnVentana(t, tAbs-0.7, tAbs+0.7) {
		if face := buscarCaraHablante(fd, tAbs-0.7, tAbs+0.7); face != nil {
			return clampX(face.CX*float64(srcW) - float64(cropW)/2)
		}
	}
	return facePosicionEn(fd, tAbs, cropW, srcW)
}

func construirCropTrackUnificado(fd *FaceData, td *TextData, t *Transcripcion,
	inicio, fin float64, cropW, srcW int) []CropKey {

	maxX := float64(srcW - cropW)
	if maxX < 0 {
		maxX = 0
	}

	const dt = 0.4
	n := int((fin-inicio)/dt) + 1
	if n < 1 {
		n = 1
	}

	x := maxX / 2.0
	if fd != nil {
		if fx := facePosicionConHabla(fd, t, inicio, cropW, srcW); fx >= 0 {
			x = fx
		}
	}

	type sample struct{ t, x float64 }
	var series []sample

	lectT0 := -1.0
	var lectX0, lectX1 float64
	var lectDur float64

	for i := 0; i < n; i++ {
		tAbs := inicio + float64(i)*dt
		var target float64 = -1

		var r *TextRegion
		if SeguirTexto {
			r = regionProminente(td, tAbs)
		}

		if r != nil {
			if r.W >= textoDominaAnchoRel {
				textoXPx := r.X * float64(srcW)
				textoWPx := r.W * float64(srcW)
				if textoWPx <= float64(cropW) {
					target = textoXPx + textoWPx/2 - float64(cropW)/2
					lectT0 = -1
				} else {
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
				lectT0 = -1
			}
		} else {
			lectT0 = -1
		}

		if target < 0 {
			if fx := facePosicionConHabla(fd, t, tAbs, cropW, srcW); fx >= 0 {
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

// ══════════════════════════════════════════════════════════════
// Fusión de planes (multi-chunk)
// ══════════════════════════════════════════════════════════════

// FusionarPlanes combina los planes parciales de todos los chunks en uno,
// elimina duplicados (clips que se solapan mucho y empiezan cerca) y
// revalida cada clip con validarClip para que respete el tope actual.
func FusionarPlanes(planes []*Plan, t *Transcripcion) *Plan {
	if len(planes) == 0 {
		return &Plan{}
	}

	var todos []ClipSpec
	var warns []string
	seenWarn := map[string]bool{}

	for _, p := range planes {
		if p == nil {
			continue
		}
		todos = append(todos, p.Clips...)
		for _, w := range p.Advertencias {
			w = strings.TrimSpace(w)
			if w != "" && !seenWarn[w] {
				seenWarn[w] = true
				warns = append(warns, w)
			}
		}
	}

	sort.Slice(todos, func(i, j int) bool {
		return todos[i].InicioSeg < todos[j].InicioSeg
	})

	var final []ClipSpec
	for _, c := range todos {
		cc := c
		validarClip(&cc, t)

		merged := false
		for i := range final {
			if clipsSonElMismo(final[i], cc) {
				merged = true
				if cc.FinSeg-cc.InicioSeg > final[i].FinSeg-final[i].InicioSeg {
					razonVieja := final[i].Razon
					hashViejos := final[i].Hashtags
					final[i] = cc
					if final[i].Razon == "" && razonVieja != "" {
						final[i].Razon = razonVieja
					}
					if len(final[i].Hashtags) == 0 && len(hashViejos) > 0 {
						final[i].Hashtags = hashViejos
					}
				} else {
					if final[i].Razon == "" && cc.Razon != "" {
						final[i].Razon = cc.Razon
					}
					if len(final[i].Hashtags) == 0 && len(cc.Hashtags) > 0 {
						final[i].Hashtags = cc.Hashtags
					}
				}
				break
			}
		}
		if !merged {
			final = append(final, cc)
		}
	}

	sort.Slice(final, func(i, j int) bool {
		return final[i].InicioSeg < final[j].InicioSeg
	})

	return &Plan{Clips: final, Advertencias: warns}
}

// clipsSonElMismo: dos clips se consideran el mismo si empiezan a menos de
// 15s el uno del otro Y su solape temporal supera el 50% del más corto.
func clipsSonElMismo(a, b ClipSpec) bool {
	if math.Abs(a.InicioSeg-b.InicioSeg) > 15 {
		return false
	}
	overlap := math.Min(a.FinSeg, b.FinSeg) - math.Max(a.InicioSeg, b.InicioSeg)
	if overlap <= 0 {
		return false
	}
	durMin := math.Min(a.FinSeg-a.InicioSeg, b.FinSeg-b.InicioSeg)
	if durMin <= 0 {
		return false
	}
	return overlap/durMin > 0.5
}

// ══════════════════════════════════════════════════════════════

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
		clip.InicioSeg, clip.FinSeg, cropX, faceData, textData, t, srcW, srcH)

	if err := EjecutarFFmpeg(args); err != nil {
		return fmt.Errorf("ffmpeg clip %d: %w", idx, err)
	}
	fmt.Printf("   ✅ clip %d: %s\n", idx+1, output)

	if EndCardImagen != "" {
		tmpOut := strings.TrimSuffix(output, ".mp4") + "_endtmp.mp4"
		if err := AplicarEndCardEnClip(output, tmpOut); err != nil {
			fmt.Printf("   ⚠️  end card falló para clip %d: %v\n", idx+1, err)
			_ = os.Remove(tmpOut)
		} else {
			_ = os.Remove(output)
			if err := os.Rename(tmpOut, output); err != nil {
				finalName := strings.TrimSuffix(output, ".mp4") + "_end.mp4"
				_ = os.Rename(tmpOut, finalName)
				fmt.Printf("   🖼️  clip %d con end card → %s\n", idx+1, filepath.Base(finalName))
			} else {
				fmt.Printf("   🖼️  end card aplicado a clip %d\n", idx+1)
			}
		}
	}
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
