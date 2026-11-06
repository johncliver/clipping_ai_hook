package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var extensionesVideo = map[string]bool{
	".mp4": true, ".mkv": true, ".mov": true, ".avi": true,
	".webm": true, ".flv": true, ".m4v": true, ".wmv": true,
	".mp3": true, ".m4a": true, ".wav": true, ".ogg": true,
	".aac": true, ".flac": true,
}

type Palabra struct {
	Texto  string  `json:"word"`
	Inicio float64 `json:"start"`
	Fin    float64 `json:"end"`
}

type Segmento struct {
	Inicio   float64   `json:"start"`
	Fin      float64   `json:"end"`
	Texto    string    `json:"text"`
	Palabras []Palabra `json:"words"`
}

type Transcripcion struct {
	Texto     string     `json:"text"`
	Idioma    string     `json:"language"`
	Segmentos []Segmento `json:"segments"`
}

func (t *Transcripcion) TextoTemporal() string {
	var b strings.Builder
	for _, s := range t.Segmentos {
		fmt.Fprintf(&b, "[%s - %s] %s\n", fmtTS(s.Inicio), fmtTS(s.Fin), strings.TrimSpace(s.Texto))
	}
	return b.String()
}

func (t *Transcripcion) Duracion() float64 {
	if len(t.Segmentos) == 0 {
		return 0
	}
	return t.Segmentos[len(t.Segmentos)-1].Fin
}

func fmtTS(seg float64) string {
	m := int(seg) / 60
	s := seg - float64(m*60)
	return fmt.Sprintf("%02d:%05.2f", m, s)
}

func ObtenerTranscripcion(input string) (*Transcripcion, error) {
	ruta := normalizarRuta(input)
	if esArchivoLocal(ruta) {
		fmt.Printf("📂 Archivo local: %s\n", ruta)
		return transcribirLocalConWhisper(ruta)
	}
	return obtenerTranscripcionYouTube(input)
}

func normalizarRuta(input string) string {
	if !strings.HasPrefix(strings.ToLower(input), "file://") {
		return input
	}
	resto := input[len("file://"):]
	resto = strings.TrimPrefix(resto, "/")
	resto = filepath.FromSlash(resto)
	if dec, err := url.PathUnescape(resto); err == nil {
		resto = dec
	}
	return resto
}

func esArchivoLocal(ruta string) bool {
	info, err := os.Stat(ruta)
	if err != nil || info.IsDir() {
		return false
	}
	return extensionesVideo[strings.ToLower(filepath.Ext(ruta))]
}

func obtenerTranscripcionYouTube(url string) (*Transcripcion, error) {
	fmt.Println("🔍 Buscando subtítulos en YouTube...")
	if t, err := obtenerSubtitulosYouTube(url); err == nil && len(t.Segmentos) > 0 {
		fmt.Println("✅ Subtítulos encontrados.")
		return t, nil
	}
	fmt.Println("⚠️  Sin subtítulos. Descargando audio con yt-dlp...")
	tmpDir, err := os.MkdirTemp("", "yt")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	audioPath := filepath.Join(tmpDir, "audio.wav")
	cmd := exec.Command("yt-dlp", "-x", "--audio-format", "wav", "-o", audioPath, url)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp audio: %w", err)
	}
	fmt.Println("🎙️  Transcribiendo con Whisper...")
	return ejecutarWhisper(audioPath)
}

func obtenerSubtitulosYouTube(url string) (*Transcripcion, error) {
	tmpDir, err := os.MkdirTemp("", "subs")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	// Intenta vtt; si falla, SRT (lo parseamos igual quitando cabecera)
	cmd := exec.Command("yt-dlp",
		"--write-subs", "--write-auto-subs",
		"--sub-langs", "en,es,es-orig,en-orig",
		"--sub-format", "vtt/srt/best",
		"--skip-download",
		"-o", filepath.Join(tmpDir, "%(id)s.%(ext)s"),
		url,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp subtítulos: %w", err)
	}
	files, _ := filepath.Glob(filepath.Join(tmpDir, "*.vtt"))
	if len(files) == 0 {
		files, _ = filepath.Glob(filepath.Join(tmpDir, "*.srt"))
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no hay subtítulos")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(files[0], ".srt") {
		return parsearSRT(string(data)), nil
	}
	return parsearVTT(string(data)), nil
}

func transcribirLocalConWhisper(ruta string) (*Transcripcion, error) {
	tmpDir, err := os.MkdirTemp("", "local")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	audioPath := filepath.Join(tmpDir, "audio.wav")
	cmd := exec.Command("ffmpeg", "-y", "-i", ruta,
		"-vn", "-acodec", "pcm_s16le", "-ar", "16000", "-ac", "1", audioPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	return ejecutarWhisper(audioPath)
}

func ejecutarWhisper(audioPath string) (*Transcripcion, error) {
	tmpDir, err := os.MkdirTemp("", "whisper_out")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	modelo := "small.en"
	cmd := exec.Command("whisper", audioPath,
		"--model", modelo, "--language", "en",
		"--output_dir", tmpDir, "--output_format", "json",
		"--word_timestamps", "True",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("whisper: %w", err)
	}
	base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	salida := filepath.Join(tmpDir, base+".json")
	data, err := os.ReadFile(salida)
	if err != nil {
		return nil, fmt.Errorf("leyendo JSON: %w", err)
	}
	var t Transcripcion
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// ---- Parser VTT ----
var reTagVTT = regexp.MustCompile(`<[^>]+>`)

func parsearVTT(vtt string) *Transcripcion {
	t := &Transcripcion{}
	var inicio, fin float64
	var buf strings.Builder
	enBloque := false
	flush := func() {
		if enBloque && buf.Len() > 0 && fin > inicio {
			t.Segmentos = append(t.Segmentos, Segmento{Inicio: inicio, Fin: fin, Texto: strings.TrimSpace(buf.String())})
		}
		buf.Reset()
		enBloque = false
	}
	for _, l := range strings.Split(vtt, "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "-->") {
			flush()
			parts := strings.SplitN(l, "-->", 2)
			if len(parts) == 2 {
				inicio = parseVTTTime(strings.TrimSpace(parts[0]))
				finStr := strings.Fields(strings.TrimSpace(parts[1]))
				if len(finStr) > 0 {
					fin = parseVTTTime(finStr[0])
					enBloque = true
				}
			}
			continue
		}
		if !enBloque {
			continue
		}
		if l == "" {
			flush()
			continue
		}
		if strings.HasPrefix(l, "WEBVTT") || strings.HasPrefix(l, "NOTE") || strings.HasPrefix(l, "STYLE") {
			continue
		}
		l = reTagVTT.ReplaceAllString(l, "")
		if buf.Len() > 0 {
			buf.WriteString(" ")
		}
		buf.WriteString(l)
	}
	flush()
	var sb strings.Builder
	for _, s := range t.Segmentos {
		sb.WriteString(s.Texto)
		sb.WriteString(" ")
	}
	t.Texto = strings.TrimSpace(sb.String())
	return t
}

func parseVTTTime(s string) float64 {
	var h, m int
	var sec float64
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 3:
		fmt.Sscanf(parts[0], "%d", &h)
		fmt.Sscanf(parts[1], "%d", &m)
		fmt.Sscanf(parts[2], "%f", &sec)
	case 2:
		fmt.Sscanf(parts[0], "%d", &m)
		fmt.Sscanf(parts[1], "%f", &sec)
	}
	return float64(h*3600+m*60) + sec
}

// ---- Parser SRT (mismo formato de tiempos, distinto separador) ----
func parsearSRT(srt string) *Transcripcion {
	// SRT usa "," en lugar de "." para milisegundos; normalizamos
	srt = strings.ReplaceAll(srt, ",", ".")
	return parsearVTT(srt)
}

// ---- Recorte de transcripción ----
func recortarTranscripcion(t *Transcripcion, inicio, fin float64) *Transcripcion {
	nueva := &Transcripcion{Idioma: t.Idioma}
	for _, s := range t.Segmentos {
		if s.Fin <= inicio || s.Inicio >= fin {
			continue
		}
		ns := s
		if ns.Inicio < inicio {
			ns.Inicio = inicio
		}
		if ns.Fin > fin {
			ns.Fin = fin
		}
		nueva.Segmentos = append(nueva.Segmentos, ns)
	}
	var sb strings.Builder
	for _, s := range nueva.Segmentos {
		sb.WriteString(s.Texto)
		sb.WriteString(" ")
	}
	nueva.Texto = strings.TrimSpace(sb.String())
	return nueva
}
