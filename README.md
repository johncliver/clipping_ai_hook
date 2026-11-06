# 🎬 Clipping IA Hook — README completo

Herramienta **en Go + Python** que automatiza el flujo completo para crear clips verticales (TikTok / Reels / Shorts) a partir de un video largo (F1, entrevistas, podcasts, etc.), con **selección de momentos hecha por DeepSeek**, **crop inteligente que sigue al hablante**, **subtítulos animados** y **validación contra reglas de campaña**.

---

## 🧭 Resumen funcional

El pipeline tiene **6 pasos encadenables**:

1. **Transcribir** — Whisper (o subtítulos de YouTube si existen).
2. **Detectar caras** — Python + MediaPipe (o Haar cascade como fallback). Marca quién habla por movimiento de boca.
3. **Detectar texto en pantalla** — Python + Tesseract (o EAST o fallback de bordes).
4. **Generar prompt** — Junta transcripción + caras + texto + reglas de la campaña en un único prompt gigante.
5. **Consultar DeepSeek** — Abre Brave con Rod, pega el prompt, espera la respuesta y la guarda.
6. **Generar clips** — Parse del JSON de DeepSeek, validación de tiempos, subtítulos ASS y FFmpeg con crop paneado.

Además hay dos utilidades fuera del pipeline:

- **🎵 Crear video con música/audio** — Fondo en loop + audio + subtítulos automáticos.
- **📱 Horizontal → Vertical** — Convierte un video horizontal a 1080×1920 (crop o fondo difuminado).

---

## 📦 Requisitos

### Obligatorios

| Herramienta | Uso |
|---|---|
| **Go ≥ 1.21** | Compilar y ejecutar el binario |
| **FFmpeg** (en `PATH`) | Recorte, escala, subtítulos, merge |
| **ffprobe** (viene con FFmpeg) | Leer duración y dimensiones |
| **yt-dlp** (en `PATH`) | Descargar video y subtítulos de YouTube |
| **Whisper** (`whisper` CLI) | Transcripción local con word timestamps |
| **Python 3.9+** | Scripts de detección de caras y texto |
| **Brave Browser** | Automatización de DeepSeek (Rod) |

### Python

```bash
pip install opencv-python numpy pytesseract mediapipe
```

> Los scripts hacen fallback en cascada:
> - Caras: `mediapipe.solutions` → `mediapipe.tasks` → `Haar cascade` (siempre funciona con OpenCV).
> - Texto: `pytesseract` → `EAST` (si existe el `.pb`) → bordes + morfología (siempre funciona).

### Opcional

- **Tesseract OCR** instalado a nivel de sistema si quieres el mejor backend de texto.
- **Brave** instalado en `C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe` (ruta que usa el código en Windows).

---

## 🚀 Instalación

```bash
git clone <repo>
cd clipping_ia_hook
go mod tidy
go build -o clipping.exe .
```

Coloca en la carpeta del proyecto:

- `campaign.txt` — reglas de la campaña (obligatorio para paso 4).
- `face_detect.py` y `text_detect.py` — deben estar junto al binario.
- `blaze_face_short_range.tflite` (opcional, se auto-descarga si usas mediapipe tasks).

---

## 🎮 Uso

```bash
./clipping
```

Se abre un **menú interactivo**:

```
1)  Transcribir                 → transcripcion.json
2)  Caras                       → caras.json
3)  Texto en pantalla           → texto.json
4)  Prompt                      → prompt_deepseek.txt
5)  DeepSeek                    → respuesta_deepseek.txt
6)  Generar clips               → clip_XX.mp4
7)  Pipeline completo (1→2→3→4→5→6)
8)  Ver estado
9)  Cambiar origen
10) Seguir texto en crop        [OFF/ON]
11) Crear video con música/audio
12) Horizontal → Vertical (1080x1920)
13) Salir
```

**Flujo recomendado**: opción `7` la primera vez. Las demás ejecuciones (ej. regenerar clips tras editar `respuesta_deepseek.txt`) usan la opción `6` directamente, ya que la cadena detecta archivos existentes y los salta.

### Ejemplo real

Al arrancar pregunta:

```
Ruta al video o URL de YouTube
```

Acepta:
- URL de YouTube (`https://youtube.com/watch?v=...`)
- Ruta local (`C:\videos\mi_video.mp4`)
- La respuesta se guarda en `procesados/source.txt` y se reutiliza.

---

## 🧠 Cómo funciona cada paso

### 1) Transcripción (`transcriber.go`)

- **Si es YouTube**: intenta primero bajar subtítulos existentes (`vtt`/`srt`). Si no hay, descarga solo el audio y pasa a Whisper.
- **Si es local**: extrae audio con FFmpeg a WAV 16k mono y usa Whisper.
- **Modelo**: `small.en`, `--word_timestamps True`.
- **Salida**: `transcripcion.json` con `segments[]` y `words[]` (start/end por palabra).

La estructura `Transcripcion` es la fuente de verdad para todo el pipeline (subtítulos, hook-anchoring, validación de tiempos).

### 2) Detección de caras (`face_detect.py` + `faces.go`)

- Muestrea el video cada **1s**.
- Detecta todas las caras, las **trackea por ID** con IoU entre frames.
- Estima **quién habla** comparando la zona de la boca entre `t` y `t+0.35s` (`_mouth_motion`). Solo marca `speaking_id` cuando hay un ganador claro (2.5× el segundo, o segundo prácticamente quieto).
- Salida: `caras.json` con samples `{t, faces:[{id,cx,cy,w,h}], speaking_id}`.

Las coordenadas son **relativas (0–1)**.

### 3) Detección de texto (`text_detect.py` + `text.go`)

- Muestrea el video cada **0.5s**.
- Detecta **bloques de texto**, agrupa renglones cercanos (`_merge_boxes`).
- Salida: `texto.json` con `{t, regions:[{x,y,w,h,conf}]}`.

> Solo se usa para el crop **si el usuario activa la opción 10** (`SeguirTexto = true`). Por defecto el crop **solo sigue caras**, y el texto se le da a DeepSeek como contexto.

### 4) Prompt (`promt.go`)

El prompt generado incluye, en orden:

1. **Reglas de la campaña** (`campaign.txt`) — con prioridad absoluta: si una idea viral las viola, se descarta.
2. **Transcripción con timestamps** absolutos.
3. **Perfil de caras** (por `t`, coordenadas del hablante).
4. **Texto en pantalla** detectado.
5. **Reglas del hook**: el clip **debe empezar** exactamente con la primera palabra del `hook_texto` (±0.3s), prohibido setup antes.
6. **Reglas de duración**: objetivo 30–90s, tope `maxClipSegundos = 170s`, `fin_seg` en final de frase natural, `hold_final_seg` (0.3–1.5s) para respirar.
7. **Sugerencia de música** por clip (`tipo`, `intensidad`, `momento_entrada_seg`, `sugerencia`).
8. **Formato JSON de salida** esperado.

También se puede limitar el rango del análisis con `Inicio` / `Fin` (HH:MM:SS o segundos), y la transcripción, caras y texto se recortan a ese rango antes de pasárselos a la IA.

### 5) DeepSeek (`automatation.go`)

- Lanza Brave con el perfil del usuario usando `go-rod`.
- Navega a `chat.deepseek.com`, espera a que cargue.
- **Cuenta las respuestas previas** para detectar la nueva (el selector `div.ds-assistant-message-main-content`).
- Escribe el prompt con `MustInput` + Enter.
- Hace **polling cada 1s** hasta que el último bloque de respuesta lleve **3 lecturas seguidas idénticas** (o ~10 min de timeout).
- Guarda la respuesta cruda en `respuesta_deepseek.txt`.

> ⚠️ Asume que el usuario **ya está logueado** en DeepSeek en Brave. No gestiona login.

### 6) Generación de clips (`clips.go`, `subs.go`, `ffmpeg.go`)

#### Parseo y validación

- `ParsearPlan` extrae el JSON de la respuesta IA (tolera texto alrededor).
- Por cada clip: `validarClip` hace **hook-anchoring**:
  - Busca las primeras 3 palabras del `hook_texto` en la transcripción.
  - Si encuentra una coincidencia a ≤ `hookTolerancia = 0.3s`, respeta el `inicio_seg`.
  - Si no, mueve `inicio_seg` al inicio real del hook menos `hookLeadIn = 0.10s`.
- **Fin natural**: busca el punto final de frase más cercano al `fin_seg` propuesto por la IA sin pasarse del tope.
- Si la IA propone un `ffmpeg_command` personalizado, se intenta ejecutar; si falla, se genera el comando estándar.

#### Crop inteligente

`construirCropTrackUnificado` mezcla caras (y opcionalmente texto) en una **serie temporal con paso 0.4s**:

- Si `SeguirTexto = OFF`: solo sigue caras. Elige el hablante o, si no hay, la cara más grande.
- Si `SeguirTexto = ON`:
  - Texto **dominante** (>35% del ancho) → gana sobre caras.
  - Texto más ancho que el crop → **paneo de lectura** a `520 px/s`.
  - Texto < 15% del ancho → se ignora.
- Los keyframes se comprimen: **cambios > 55 px** con **≥ 1.1s** entre ellos, máximo 25.

`construirExpresionCrop` genera una expresión FFmpeg anidada con `if/lt/lerp` para hacer **transiciones de 0.5s** centradas en cada keyframe.

#### Subtítulos ASS (`subs.go`)

- Fuente `Arial Black`, 92pt, alineación bottom-center con margen `MarginV=420` (para Reels/Shorts).
- Agrupa palabras en **chunks de ≤ 3 palabras** (corta en puntuación).
- Cada palabra se renderiza **con karaoke**: la palabra activa se pinta en amarillo `&H0000FFFF&`, el resto en blanco.
- `GenerarASSClip` traduce los tiempos a **relativos al clip** (`[inicio, fin]`).
- Tiempos en formato `H:MM:SS.cc` (centésimas).

#### FFmpeg

`ConstruirComandoClip`:
- `-ss inicio -to fin` (recorte preciso).
- `crop` con expresión `x` dinámica, `y=0`.
- `scale` a **1080×1920**.
- `ass='ruta_escapada'` para subtítulos.
- `libx264 -crf 20 -preset veryfast`, `aac 192k`, `+faststart`.

Salida: `procesados/clip_XX.mp4`.

---

## 🎁 Utilidades extra

### 🎵 Music video (`music_video.go`)

Combina:
- Video de fondo (loop infinito).
- Pista de audio (define duración total).
- Subtítulos automáticos generados con Whisper sobre el **audio**, no sobre el video.

Útil para editar audio de podcast / voz y montarlo sobre un fondo neutro.

### 📱 Horizontal → Vertical (`vertical.go`)

Dos modos:

- **`crop`** (default): recorta los lados, llena el frame. Pierde ~68% del ancho en 16:9 → 9:16. Ideal para Reels/TikTok.
- **`blur`**: fondo difuminado (`gblur sigma=30`) + video completo centrado encima. No pierde nada.

---

## 📁 Estructura del proyecto

```
.
├── main.go               # Menú interactivo
├── menu.go               # Render del menú + cabecera
├── chain.go              # Encadenamiento automático (pipeline)
├── steps.go              # Cada paso individual (1..6)
├── status.go             # Ver estado de archivos intermedios
├── transcriber.go        # Whisper + parser VTT/SRT
├── youtube.go            # Descarga de video (yt-dlp)
├── face_detect.py        # Detección de caras (Python)
├── faces.go              # Structs + construcción de crop track de caras
├── text_detect.py        # Detección de texto (Python)
├── text.go               # Structs + carga/guardado
├── subs.go               # Generación de .ass con karaoke
├── clips.go              # Validación, hook-anchoring, crop unificado
├── ffmpeg.go             # Construcción y ejecución de comandos FFmpeg
├── promt.go              # Construcción del prompt para DeepSeek
├── automatation.go       # Automatización de Brave + DeepSeek (Rod)
├── music_video.go        # Fondo + audio + subtítulos
├── vertical.go           # Horizontal → 1080×1920
├── helpers.go            # Utilidades (parseo de tiempos, filtros)
├── color_windows.go      # Habilita ANSI en consolas Windows
├── color_other.go        # Stub no-Windows
├── go.mod
└── campaign.txt          # Reglas de campaña (editable)
```

Salida en `procesados/`:

```
source.txt                  # Ruta/URL del video origen
video.mp4                   # Video descargado (si aplica)
transcripcion.json          # Transcripción con word timestamps
caras.json                  # Samples de caras + speaking_id
texto.json                  # Regiones de texto detectadas
prompt_deepseek.txt         # Prompt enviado a DeepSeek
respuesta_deepseek.txt      # JSON devuelto por DeepSeek
clip_01.mp4, clip_02.mp4…   # Clips finales
clip_01.ass, clip_02.ass…   # Subtítulos por clip
music_video_transcripcion.json
music_video.ass
music_video.mp4
```

---

## ⚙️ Configuración clave (constantes)

| Constante | Archivo | Valor | Significado |
|---|---|---|---|
| `maxClipSegundos` | `clips.go` | `170.0` | Tope duro por clip (2:50) |
| `hookLeadIn` | `clips.go` | `0.10` | Margen antes del hook |
| `hookTolerancia` | `clips.go` | `0.30` | Tolerancia de alineación |
| `paneoLecturaPxPerSec` | `clips.go` | `520.0` | Velocidad de paneo al leer texto |
| `textoMinAnchoRel` | `clips.go` | `0.15` | Ancho mínimo para considerar texto |
| `textoDominaAnchoRel` | `clips.go` | `0.35` | Ancho a partir del cual el texto domina el crop |
| `SeguirTexto` | `clips.go` | `false` | Toggle del menú (opción 10) |
| `fuenteSubs` | `subs.go` | `Arial Black` | Fuente de los subtítulos |
| `modelo` (Whisper) | `transcriber.go` | `small.en` | Cambia aquí si necesitas otro idioma |
| `maxPalabrasChunk` | `subs.go` | `3` | Palabras por chunk de subtítulo |

---

## 🧪 Ejemplo de flujo real

```text
$ ./clipping
📂 Origen:  https://youtube.com/watch?v=XXXX
📁 Salida:  procesados/

> 7   # pipeline completo

🎬 Transcribiendo...
✅ 128 segmentos → procesados/transcripcion.json

👤 Detectando caras...
✅ 540 muestras → procesados/caras.json

📝 Detectando texto en pantalla...
✅ 87 muestras con texto → procesados/texto.json

Prompt → procesados/prompt_deepseek.txt (48213 chars)

🤖 Consultando DeepSeek (abre Brave)...
✅ Respuesta → procesados/respuesta_deepseek.txt (3120 chars)

▶ auto: paso 6 (clips)
  → Clip 1: [51.2s - 145.0s] Genuinely, I am up for anything
  → Clip 2: [161.1s - 250.3s] What a day
  ...
✅ Clips en procesados/
```

---

## ⚠️ Notas y limitaciones

- **Windows-first**: `automatation.go` asume rutas de Brave en Windows (`LOCALAPPDATA`, `Program Files`). En Linux/macOS hay que ajustar `braveBin` y `braveUserDataDir`.
- **DeepSeek requiere sesión activa** en Brave; el código no gestiona login.
- **Whisper** debe estar instalado como CLI (`pip install openai-whisper`).
- El script asume `python` en `PATH` (no `python3`). En Linux/macOS puede requerir alias.
- Los clips se numeran desde `clip_01.mp4`. Si ya existen, el paso 6 los **salta** (borra el directorio o cambia el nombre si quieres regenerarlos).
- `campaign.txt` es la fuente de verdad de las reglas. Si cambias de campaña, cambia el archivo (opción 4 lo leerá de nuevo solo si regeneras el prompt).

---

## 🔧 Comandos útiles

```bash
# Solo regenerar clips desde una respuesta ya existente
# (borra antes los clips viejos)
rm procesados/clip_*.mp4
./clipping   # opción 6

# Ver estado de archivos intermedios
./clipping   # opción 8

# Convertir cualquier video a vertical (sin pipeline completo)
./clipping   # opción 12

# Cambiar origen
./clipping   # opción 9
```

---

## 📝 Licencia

Sin licencia explícita en el repo. Uso personal / interno.