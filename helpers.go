package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func loadTranscripcionJSON(path string) (*Transcripcion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Transcripcion
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("JSON inválido: %w", err)
	}
	return &t, nil
}

func parseTS(s string, def float64) float64 {
	if s == "" {
		return def
	}
	if !strings.Contains(s, ":") {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return def
		}
		return v
	}
	parts := strings.Split(s, ":")
	var secs float64
	for _, p := range parts {
		v, _ := strconv.ParseFloat(p, 64)
		secs = secs*60 + v
	}
	return secs
}

func filtrarCaras(fd *FaceData, inicio, fin float64) *FaceData {
	out := &FaceData{Width: fd.Width, Height: fd.Height, FPS: fd.FPS, Duration: fd.Duration}
	for _, s := range fd.Samples {
		if s.T >= inicio && s.T <= fin {
			out.Samples = append(out.Samples, s)
		}
	}
	return out
}

func filtrarTexto(td *TextData, inicio, fin float64) *TextData {
	out := &TextData{Width: td.Width, Height: td.Height, FPS: td.FPS, Duration: td.Duration}
	for _, s := range td.Samples {
		if s.T >= inicio && s.T <= fin {
			out.Samples = append(out.Samples, s)
		}
	}
	return out
}
