package main

import (
	"fmt"
	"os"
	"os/exec"
)

func ConstruirComandoClip(input, output, assPath string,
	inicio, fin, cropX float64,
	faceData *FaceData, textData *TextData,
	srcW, srcH int) []string {

	targetW, targetH := 1080, 1920
	if srcW <= 0 || srcH <= 0 {
		srcW, srcH = 1920, 1080
	}

	cropH := srcH
	cropW := srcH * targetW / targetH
	if cropW > srcW {
		cropW = srcW
		cropH = srcW * targetH / targetW
	}
	if cropW%2 != 0 {
		cropW--
	}
	if cropH%2 != 0 {
		cropH--
	}

	// Track unificado: caras + texto (paneo de lectura)
	keys := construirCropTrackUnificado(faceData, textData, inicio, fin, cropW, srcW)

	cropDefault := int(cropX*float64(srcW)) - cropW/2
	if cropDefault < 0 {
		cropDefault = 0
	}
	if cropDefault > srcW-cropW {
		cropDefault = srcW - cropW
	}
	xExpr := construirExpresionCrop(keys, cropDefault)

	cropFilter := fmt.Sprintf("crop=%d:%d:x='%s':y=0", cropW, cropH, xExpr)
	scaleFilter := fmt.Sprintf("scale=%d:%d", targetW, targetH)

	var vf string
	if assPath != "" {
		ass := escapePathFFmpeg(assPath)
		vf = fmt.Sprintf("%s,%s,ass='%s'", cropFilter, scaleFilter, ass)
	} else {
		vf = fmt.Sprintf("%s,%s", cropFilter, scaleFilter)
	}

	return []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", inicio),
		"-to", fmt.Sprintf("%.3f", fin),
		"-i", input,
		"-vf", vf,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
		"-c:a", "aac", "-b:a", "192k",
		"-movflags", "+faststart",
		output,
	}
}

func escapePathFFmpeg(p string) string {
	return filepathToSlash(p)
}

func filepathToSlash(p string) string {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case '\\':
			out = append(out, '/')
		case ':':
			out = append(out, '\\', ':')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

func EjecutarFFmpeg(args []string) error {
	fmt.Println("   $ ffmpeg", joinArgs(args))
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func joinArgs(a []string) string {
	s := ""
	for i, x := range a {
		if i > 0 {
			s += " "
		}
		if contains(x, " ") {
			s += `"` + x + `"`
		} else {
			s += x
		}
	}
	return s
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
