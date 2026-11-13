package main

import (
	"clipping_ia_hook/internal/ui"
	"fmt"
	"strconv"
	"strings"
)

// CropHibrido: toggle del menú (opción 15).
// ON  → cara: crop 9:16 siguiendo al hablante
//
//	sin cara: video 4:5 centrado sobre blur vertical
//
// OFF → crop 9:16 puro clásico (construirClipCropPuro)
var CropHibrido = true

const (
	hibridoCaraMinRel = 0.10
	hibridoCaraDomRel = 0.20
	hibridoHistSeg    = 2.0
)

// ── Crop horizontal configurable (foreground del modo blur) ──
//
// hibridoForegroundRel = ancho / alto del foreground 4:X
//
//	0.5625 → 9:16  (vertical, casi como el crop normal)
//	0.75   → 3:4   (casi vertical)
//	0.8    → 4:5   (default — balance horizontal/vertical)
//	1.0    → 1:1   (cuadrado)
//	1.333  → 4:3   (horizontal)
//	1.778  → 16:9  (horizontal completo)
var (
	CropHorizontalPreset = "4:5"     // nombre legible para el menú
	hibridoForegroundRel = 4.0 / 5.0 // valor numérico real
)

// presetCropHorizontal aplica un preset por nombre. Devuelve false si no existe.
func presetCropHorizontal(nombre string) bool {
	switch nombre {
	case "9:16":
		hibridoForegroundRel = 9.0 / 16.0
	case "3:4":
		hibridoForegroundRel = 3.0 / 4.0
	case "4:5":
		hibridoForegroundRel = 4.0 / 5.0
	case "1:1":
		hibridoForegroundRel = 1.0
	case "4:3":
		hibridoForegroundRel = 4.0 / 3.0
	case "16:9":
		hibridoForegroundRel = 16.0 / 9.0
	default:
		return false
	}
	CropHorizontalPreset = nombre
	return true
}

// configurarCropHorizontal permite elegir el tamaño del foreground cuando
// el modo híbrido cae a blur (sin cara relevante). Solo afecta al modo blur,
// no al crop 9:16 que sigue a la cara.
func configurarCropHorizontal() {
	fmt.Println(ui.Col(ui.Cyan, "🖼️  Tamaño del crop horizontal (modo blur)"))
	fmt.Println()
	fmt.Println(ui.Col(ui.Gray,
		"  Afecta solo cuando el modo híbrido NO detecta cara y usa el blur."))
	fmt.Println(ui.Col(ui.Gray,
		"  Actual: "+CropHorizontalPreset+
			fmt.Sprintf(" (foreground rel = %.3f)", hibridoForegroundRel)))
	fmt.Println()

	fmt.Println(ui.Col(ui.White, "Presets:"))
	fmt.Println(ui.Col(ui.Gray, "  1) 9:16   vertical (casi como el crop de cara)"))
	fmt.Println(ui.Col(ui.Gray, "  2) 3:4    casi vertical"))
	fmt.Println(ui.Col(ui.Gray, "  3) 4:5    equilibrado (default)"))
	fmt.Println(ui.Col(ui.Gray, "  4) 1:1    cuadrado"))
	fmt.Println(ui.Col(ui.Gray, "  5) 4:3    horizontal"))
	fmt.Println(ui.Col(ui.Gray, "  6) 16:9   horizontal completo"))
	fmt.Println(ui.Col(ui.Gray, "  7) custom (introducir ratio a mano)"))
	op := ui.Ask("Elige 1-7 (vacío = no cambiar)")

	switch op {
	case "1":
		presetCropHorizontal("9:16")
	case "2":
		presetCropHorizontal("3:4")
	case "3":
		presetCropHorizontal("4:5")
	case "4":
		presetCropHorizontal("1:1")
	case "5":
		presetCropHorizontal("4:3")
	case "6":
		presetCropHorizontal("16:9")
	case "7":
		cStr := ui.Ask("Ratio ancho/alto (ej. 0.8 = 4:5, 1.0 = 1:1, 1.778 = 16:9)")
		if v, err := strconv.ParseFloat(cStr, 64); err == nil && v >= 0.3 && v <= 2.5 {
			hibridoForegroundRel = v
			CropHorizontalPreset = fmt.Sprintf("custom %.3f", v)
		} else {
			ui.ErrLn("Valor inválido (rango 0.3 - 2.5)")
			return
		}
	case "":
		return
	default:
		ui.ErrLn("Opción inválida")
		return
	}

	ui.OKLn(fmt.Sprintf("Crop horizontal → %s (rel = %.3f)",
		CropHorizontalPreset, hibridoForegroundRel))
	ui.OKLn("Aplicado a los próximos clips que generes (modo blur)")
}

type intervaloCara struct {
	Inicio, Fin float64
}

// detectarIntervalosCaraRelevante devuelve tramos donde hay una cara
// lo bastante grande para seguirla. Histéresis de 2 muestras.
func detectarIntervalosCaraRelevante(fd *FaceData, inicio, fin float64) []intervaloCara {
	if fd == nil || len(fd.Samples) == 0 {
		return nil
	}

	type muestra struct {
		t   float64
		hay bool
	}
	var ms []muestra
	for _, s := range fd.Samples {
		if s.T < inicio-2 || s.T > fin+2 {
			continue
		}
		hay := false
		for _, f := range s.Faces {
			if f.W >= hibridoCaraMinRel {
				hay = true
			}
			if f.W >= hibridoCaraDomRel {
				hay = true
			}
		}
		ms = append(ms, muestra{t: s.T, hay: hay})
	}
	if len(ms) == 0 {
		return nil
	}

	type tramo struct {
		ini, fin float64
		hay      bool
	}
	var raw []tramo
	cur := tramo{ini: ms[0].t, hay: ms[0].hay}
	cambio := 0
	for i := 1; i < len(ms); i++ {
		if ms[i].hay == cur.hay {
			cambio = 0
			continue
		}
		cambio++
		if cambio >= 2 {
			cur.fin = ms[i].t
			raw = append(raw, cur)
			cur = tramo{ini: ms[i].t, hay: ms[i].hay}
			cambio = 0
		}
	}
	cur.fin = ms[len(ms)-1].t
	raw = append(raw, cur)

	var out []intervaloCara
	for _, t := range raw {
		if !t.hay {
			continue
		}
		a, b := t.ini, t.fin
		if a < inicio {
			a = inicio
		}
		if b > fin {
			b = fin
		}
		if b-a < hibridoHistSeg {
			continue
		}
		out = append(out, intervaloCara{Inicio: a, Fin: b})
	}
	return out
}

// construirExpresionEnable arma "between(t,a,b)+between(t,c,d)+..."
// con tiempos RELATIVOS al inicio del clip (-ss antes de -i resetea a 0).
func construirExpresionEnable(ivs []intervaloCara, inicio float64) string {
	if len(ivs) == 0 {
		return ""
	}
	var partes []string
	for _, iv := range ivs {
		a := iv.Inicio - inicio
		b := iv.Fin - inicio
		if a < 0 {
			a = 0
		}
		if b <= a {
			continue
		}
		partes = append(partes, fmt.Sprintf("between(t,%.3f,%.3f)", a, b))
	}
	return strings.Join(partes, "+")
}

// construirFiltergraphHibrido SIEMPRE devuelve un filtergraph válido:
//   - Sin caras relevantes → blur puro (video 4:5 sobre fondo 9:16 borroso)
//   - Con caras → blur base + overlay crop 9:16 cuando la cara existe
func construirFiltergraphHibrido(inicio, fin, cropX float64,
	faceData *FaceData, textData *TextData, t *Transcripcion,
	srcW, srcH, targetW, targetH int, assPath string) string {

	// Crop 9:16 (stream A)
	cropH9 := srcH
	cropW9 := srcH * targetW / targetH
	if cropW9 > srcW {
		cropW9 = srcW
		cropH9 = srcW * targetH / targetW
	}
	if cropW9%2 != 0 {
		cropW9--
	}
	if cropH9%2 != 0 {
		cropH9--
	}

	// Foreground 4:5 (stream B fg)
	fgH := srcH
	fgW := int(float64(srcH) * hibridoForegroundRel)
	if fgW > srcW {
		fgW = srcW
	}
	if fgW%2 != 0 {
		fgW--
	}
	if fgH%2 != 0 {
		fgH--
	}

	ivs := detectarIntervalosCaraRelevante(faceData, inicio, fin)
	enable := construirExpresionEnable(ivs, inicio)

	var sb strings.Builder

	if enable == "" {
		// Sin caras → solo blur puro (video 4:5 + fondo difuminado)
		sb.WriteString("[0:v]split=2[src_bg1][src_bg2];")
		fmt.Fprintf(&sb,
			"[src_bg1]scale=%d:%d:force_original_aspect_ratio=increase,"+
				"crop=%d:%d,gblur=sigma=30[bg];",
			targetW, targetH, targetW, targetH)
		fmt.Fprintf(&sb,
			"[src_bg2]crop=%d:%d:x=(iw-%d)/2:y=(ih-%d)/2,"+
				"scale=%d:-2:flags=lanczos[fg];",
			fgW, fgH, fgW, fgH, targetW)
		sb.WriteString("[bg][fg]overlay=(W-w)/2:(H-h)/2:shortest=1,setsar=1[v_mix];")
	} else {
		// Con caras → base blur + overlay crop 9:16 con enable
		keys := construirCropTrackUnificado(faceData, textData, t,
			inicio, fin, cropW9, srcW)
		cropDefault := int(cropX*float64(srcW)) - cropW9/2
		if cropDefault < 0 {
			cropDefault = 0
		}
		if cropDefault > srcW-cropW9 {
			cropDefault = srcW - cropW9
		}
		xExpr := construirExpresionCrop(keys, cropDefault)

		sb.WriteString("[0:v]split=3[src_crop][src_bg1][src_bg2];")
		fmt.Fprintf(&sb,
			"[src_crop]crop=%d:%d:x='%s':y=0,scale=%d:%d:flags=lanczos,setsar=1[v_crop];",
			cropW9, cropH9, xExpr, targetW, targetH)
		fmt.Fprintf(&sb,
			"[src_bg1]scale=%d:%d:force_original_aspect_ratio=increase,"+
				"crop=%d:%d,gblur=sigma=30[bg];",
			targetW, targetH, targetW, targetH)
		fmt.Fprintf(&sb,
			"[src_bg2]crop=%d:%d:x=(iw-%d)/2:y=(ih-%d)/2,"+
				"scale=%d:-2:flags=lanczos[fg];",
			fgW, fgH, fgW, fgH, targetW)
		sb.WriteString("[bg][fg]overlay=(W-w)/2:(H-h)/2:shortest=1,setsar=1[v_blur];")
		fmt.Fprintf(&sb,
			"[v_blur][v_crop]overlay=0:0:enable='%s'[v_mix];", enable)
	}

	// Subtítulos encima del resultado
	if assPath != "" {
		ass := escapePathFFmpeg(assPath)
		fmt.Fprintf(&sb, "[v_mix]ass='%s'[vout]", ass)
	} else {
		sb.WriteString("[v_mix]null[vout]")
	}

	return sb.String()
}
