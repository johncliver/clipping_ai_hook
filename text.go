package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

type TextRegion struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	W    float64 `json:"w"`
	H    float64 `json:"h"`
	Conf int     `json:"conf"`
}

type TextSample struct {
	T       float64      `json:"t"`
	Regions []TextRegion `json:"regions"`
}

type TextData struct {
	Width    int          `json:"width"`
	Height   int          `json:"height"`
	FPS      float64      `json:"fps"`
	Duration float64      `json:"duration"`
	Samples  []TextSample `json:"samples"`
}

func DetectarTexto(videoPath string) (*TextData, error) {
	cmd := exec.Command("python", "text_detect.py", videoPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("python text_detect.py: %v\n%s", err, stderr.String())
	}
	var td TextData
	if err := json.Unmarshal(stdout.Bytes(), &td); err != nil {
		return nil, fmt.Errorf("parseando texto: %v\nstdout=%s\nstderr=%s",
			err, stdout.String(), stderr.String())
	}
	return &td, nil
}

func GuardarTexto(td *TextData, path string) error {
	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func CargarTexto(path string) (*TextData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var td TextData
	if err := json.Unmarshal(data, &td); err != nil {
		return nil, err
	}
	return &td, nil
}
