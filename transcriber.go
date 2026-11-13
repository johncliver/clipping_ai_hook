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

// Directorio persistente donde se guardan los .ass generados
const assOutputDir = "./subs"

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

// ══════════════════════════════════════════════════════════════
// YouTube: subtítulos en → es → Whisper (fallback)
// ══════════════════════════════════════════════════════════════

func obtenerTranscripcionYouTube(url string) (*Transcripcion, error) {
	fmt.Println("🔍 Buscando subtítulos en YouTube (prioridad: inglés)...")
	if t, err := obtenerSubtitulosYouTube(url); err == nil && len(t.Segmentos) > 0 {
		fmt.Printf("✅ Subtítulos encontrados (idioma=%s).\n", t.Idioma)
		return t, nil
	}
	fmt.Println("⚠️  Sin subtítulos en inglés ni español. Descargando audio con yt-dlp...")
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

// obtenerSubtitulosYouTube:
//  1. yt-dlp --write-sub --write-auto-sub --sub-lang en
//  2. si no hay → yt-dlp ... --sub-lang es
//  3. convierte el .vtt/.srt elegido a .ass con ffmpeg
//  4. persiste el .ass en ./subs/ para uso posterior
//  5. parsea el .vtt/.srt para construir la Transcripcion
func obtenerSubtitulosYouTube(url string) (*Transcripcion, error) {
	tmpDir, err := os.MkdirTemp("", "subs")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	// 1) Inglés primero
	vttPath, lang := descargarSubtitulosIdioma(url, tmpDir, "en")

	// 2) Fallback español
	if vttPath == "" {
		fmt.Println("   ⚠️  Sin subtítulos en inglés, probando español...")
		vttPath, lang = descargarSubtitulosIdioma(url, tmpDir, "es")
	}

	if vttPath == "" {
		return nil, fmt.Errorf("no hay subtítulos en inglés ni español")
	}
	fmt.Printf("🎯 Subtítulo elegido: %s (idioma=%s)\n", filepath.Base(vttPath), lang)

	// 3) Convertir a .ass
	assPath := strings.TrimSuffix(vttPath, filepath.Ext(vttPath)) + ".ass"
	if err := convertirAASS(vttPath, assPath); err != nil {
		fmt.Printf("⚠️  No se pudo convertir a .ass: %v\n", err)
	} else {
		// 4) Persistir en ./subs/
		if saved, err := persistirASS(assPath, lang); err != nil {
			fmt.Printf("⚠️  No se pudo guardar .ass: %v\n", err)
		} else {
			fmt.Printf("💾 .ass guardado en: %s\n", saved)
		}
	}

	// 5) Parsear VTT/SRT → Transcripcion
	data, err := os.ReadFile(vttPath)
	if err != nil {
		return nil, err
	}
	var t *Transcripcion
	if strings.HasSuffix(strings.ToLower(vttPath), ".srt") {
		t = parsearSRT(string(data))
	} else {
		t = parsearVTT(string(data))
	}
	t.Idioma = lang
	return t, nil
}

// descargarSubtitulosIdioma ejecuta el comando yt-dlp pedido y devuelve la
// ruta del archivo .vtt (o .srt) del idioma solicitado.
//
//	yt-dlp --write-sub --write-auto-sub --sub-lang <lang> --skip-download \
//	       -o "<tmpDir>/%(id)s.%(ext)s" <url>
func descargarSubtitulosIdioma(url, tmpDir, lang string) (string, string) {
	// Probar primero la pista "original" (auto-generada), luego la estándar
	candidatos := []string{lang + "-orig", lang}

	for _, idioma := range candidatos {
		fmt.Printf("   🔎 Probando --sub-lang %s ...\n", idioma)

		cmd := exec.Command("yt-dlp",
			"--write-sub", "--write-auto-sub", // 👈 ¡CRUCIAL! Sin esto no hay en-orig
			"--sub-lang", idioma,
			"--sub-format", "vtt/srt/best",
			"--skip-download",
			"--remote-components", "ejs:github",
			"--impersonate", "chrome",
			"--cookies", "cookies.txt",
			"--extractor-retries", "5",
			"--sleep-subtitles", "10",
			"-o", filepath.Join(tmpDir, "%(id)s.%(ext)s"),
			url,
		)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("   ⚠️  yt-dlp falló para %q: %v\n", idioma, err)
			continue
		}

		// Buscar el archivo generado (.vtt o .srt)
		var vtts, srts []string
		for _, ext := range []string{"*.vtt", "*.srt"} {
			files, _ := filepath.Glob(filepath.Join(tmpDir, ext))
			for _, f := range files {
				base := strings.ToLower(filepath.Base(f))
				stem := strings.TrimSuffix(base, strings.ToLower(filepath.Ext(base)))
				parts := strings.Split(stem, ".")
				for _, p := range parts {
					if strings.HasPrefix(p, idioma) {
						if strings.HasSuffix(base, ".vtt") {
							vtts = append(vtts, f)
						} else {
							srts = append(srts, f)
						}
						break
					}
				}
			}
		}
		if len(vtts) > 0 {
			return vtts[0], idioma
		}
		if len(srts) > 0 {
			return srts[0], idioma
		}
		fmt.Printf("   ℹ️  yt-dlp no generó archivo para %q, probando siguiente...\n", idioma)
	}

	return "", ""
}

// convertirAASS: ffmpeg -i <vtt|srt> -f ass <out.ass>
func convertirAASS(input, output string) error {
	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", input,
		"-f", "ass",
		output,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg → .ass: %w", err)
	}
	return nil
}

// persistirASS copia el .ass generado a ./subs/<base>.<lang>.ass
func persistirASS(assPath, lang string) (string, error) {
	if err := os.MkdirAll(assOutputDir, 0755); err != nil {
		return "", err
	}
	stem := strings.TrimSuffix(filepath.Base(assPath), filepath.Ext(assPath))
	final := filepath.Join(assOutputDir, stem+"."+lang+".ass")

	data, err := os.ReadFile(assPath)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(final, data, 0644); err != nil {
		return "", err
	}
	return final, nil
}

// ══════════════════════════════════════════════════════════════
// Whisper (fallback sin subtítulos)
// ══════════════════════════════════════════════════════════════

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
	if t.Idioma == "" {
		t.Idioma = "en"
	}
	return &t, nil
}

// ══════════════════════════════════════════════════════════════
// Parser VTT
// ══════════════════════════════════════════════════════════════

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

// ══════════════════════════════════════════════════════════════
// Parser SRT
// ══════════════════════════════════════════════════════════════

func parsearSRT(srt string) *Transcripcion {
	srt = strings.ReplaceAll(srt, ",", ".")
	return parsearVTT(srt)
}

// ══════════════════════════════════════════════════════════════
// Recorte de transcripción
// ══════════════════════════════════════════════════════════════

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
