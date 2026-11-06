package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

type FaceDetect struct {
	ID          int     `json:"id"`
	CX          float64 `json:"cx"`
	CY          float64 `json:"cy"`
	W           float64 `json:"w"`
	H           float64 `json:"h"`
	MouthMotion float64 `json:"mouth_motion,omitempty"`
}

type FaceSample struct {
	T          float64      `json:"t"`
	Faces      []FaceDetect `json:"faces"`
	SpeakingID *int         `json:"speaking_id"`
}

type FaceData struct {
	Width    int          `json:"width"`
	Height   int          `json:"height"`
	FPS      float64      `json:"fps"`
	Duration float64      `json:"duration"`
	Samples  []FaceSample `json:"samples"`
}

func DetectarCaras(videoPath string) (*FaceData, error) {
	cmd := exec.Command("python", "face_detect.py", videoPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("python face_detect.py: %v\n%s", err, stderr.String())
	}
	var fd FaceData
	if err := json.Unmarshal(stdout.Bytes(), &fd); err != nil {
		return nil, fmt.Errorf("parseando salida de caras: %v\nstdout=%s\nstderr=%s",
			err, stdout.String(), stderr.String())
	}
	return &fd, nil
}

func GuardarCaras(fd *FaceData, path string) error {
	data, err := json.MarshalIndent(fd, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func CargarCaras(path string) (*FaceData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fd FaceData
	if err := json.Unmarshal(data, &fd); err != nil {
		return nil, err
	}
	return &fd, nil
}

// ---- Crop track (sigue al hablante) ----

type CropKey struct {
	T   float64 // segundos desde el inicio del clip
	XPx int     // posición x (px) en el video fuente
}

func elegirCara(s FaceSample) *FaceDetect {
	if s.SpeakingID != nil {
		for i := range s.Faces {
			if s.Faces[i].ID == *s.SpeakingID {
				return &s.Faces[i]
			}
		}
	}
	if len(s.Faces) == 0 {
		return nil
	}
	biggest := &s.Faces[0]
	for i := range s.Faces {
		if s.Faces[i].W > biggest.W {
			biggest = &s.Faces[i]
		}
	}
	return biggest
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// construirCropTrack devuelve keyframes (t, x) donde el crop debe cambiar.
// Solo registra transiciones significativas con separación mínima.
func construirCropTrack(fd *FaceData, inicio, fin float64, cropW, srcW int) []CropKey {
	if fd == nil || len(fd.Samples) == 0 || cropW <= 0 {
		return nil
	}
	maxX := srcW - cropW
	if maxX < 0 {
		maxX = 0
	}

	prevX := srcW/2 - cropW/2
	if prevX < 0 {
		prevX = 0
	}
	if prevX > maxX {
		prevX = maxX
	}

	const (
		umbralPx    = 60  // px de cambio para considerar transición
		minInterval = 1.2 // seg entre transiciones (evita ruido)
		maxKeys     = 20
	)

	// Pasada 1: posición por muestra
	type rawKey struct {
		t float64
		x int
	}
	var raw []rawKey
	for _, s := range fd.Samples {
		if s.T < inicio || s.T > fin {
			continue
		}
		x := prevX
		if face := elegirCara(s); face != nil {
			x = int(face.CX*float64(srcW)) - cropW/2
			if x < 0 {
				x = 0
			}
			if x > maxX {
				x = maxX
			}
		}
		prevX = x
		raw = append(raw, rawKey{t: s.T - inicio, x: x})
	}
	if len(raw) == 0 {
		return nil
	}

	// Pasada 2: solo transiciones significativas
	keys := []CropKey{{T: raw[0].t, XPx: raw[0].x}}
	prevX = raw[0].x
	lastChangeT := raw[0].t
	for _, r := range raw[1:] {
		if absInt(r.x-prevX) > umbralPx && r.t-lastChangeT > minInterval {
			keys = append(keys, CropKey{T: r.t, XPx: r.x})
			prevX = r.x
			lastChangeT = r.t
		}
	}
	if len(keys) > maxKeys {
		keys = keys[:maxKeys]
	}
	return keys
}

// construirExpresionCrop arma una expresión FFmpeg para crop.x con pan suave:
//
//	if(lt(t,Tstart), prevX,
//	   if(lt(t,Tend), lerp(prevX,nextX,(t-Tstart)/dur),
//	      <siguiente>))
//
// La transición dura 0.5s centrada en cada keyframe T (excepto el primero).
func construirExpresionCrop(keys []CropKey, cropDefault int) string {
	if len(keys) == 0 {
		return fmt.Sprintf("%d", cropDefault)
	}
	if len(keys) == 1 {
		return fmt.Sprintf("%d", keys[0].XPx)
	}

	const transDur = 0.5 // segundos de pan suave

	// Construcción backward: cada iteración envuelve la anterior
	expr := fmt.Sprintf("%d", keys[len(keys)-1].XPx)
	for i := len(keys) - 2; i >= 0; i-- {
		T := keys[i+1].T
		prevX := keys[i].XPx
		nextX := keys[i+1].XPx

		if prevX == nextX {
			expr = fmt.Sprintf("if(lt(t,%.3f),%d,%s)", T, prevX, expr)
			continue
		}

		Tstart := T - transDur/2
		Tend := T + transDur/2
		if Tstart < 0 {
			Tstart = 0
			Tend = transDur
		}
		// Si la próxima transición empieza antes de Tend, acortar esta
		if i+2 < len(keys) {
			nextStart := keys[i+2].T - transDur/2
			if Tend > nextStart {
				Tend = nextStart
			}
		}
		dur := Tend - Tstart
		if dur < 0.1 {
			// Transiciones muy cercanas: corte duro para no atropellarse
			expr = fmt.Sprintf("if(lt(t,%.3f),%d,%s)", T, prevX, expr)
			continue
		}

		lerpE := fmt.Sprintf("lerp(%d,%d,(t-%.3f)/%.3f)",
			prevX, nextX, Tstart, dur)
		expr = fmt.Sprintf("if(lt(t,%.3f),%d,if(lt(t,%.3f),%s,%s))",
			Tstart, prevX, Tend, lerpE, expr)
	}
	return expr
}
