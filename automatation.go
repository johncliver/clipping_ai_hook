package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
)

// errRateLimit indica que DeepSeek bloqueó el envío por exceso de frecuencia.
var errRateLimit = errors.New("rate limit de DeepSeek alcanzado (Messages too frequent)")

// errJSONInvalido indica que la respuesta no era JSON parseable.
// NO reintentamos en el mismo chat: reenviar el mismo prompt confunde al modelo.
var errJSONInvalido = errors.New("respuesta de DeepSeek no es JSON parseable")

func esRateLimitRespuesta(s string) bool {
	if len(s) > 300 {
		return false
	}
	l := strings.ToLower(s)
	return strings.Contains(l, "too frequent") ||
		strings.Contains(l, "too many requests") ||
		strings.Contains(l, "try again later")
}

// ══════════════════════════════════════════════════════════════
// Sesión persistente: UNA página, N prompts en el mismo chat.
// ══════════════════════════════════════════════════════════════

type SesionDeepSeek struct {
	browser       *rod.Browser
	page          *rod.Page
	numRespuestas int // nº de respuestas del asistente ya vistas en este chat
}

// NuevaSesionDeepSeek lanza Brave, abre chat.deepseek.com y deja la página lista.
// Llamar UNA sola vez y reutilizar para todos los chunks.
func NuevaSesionDeepSeek() (*SesionDeepSeek, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return nil, fmt.Errorf("LOCALAPPDATA no definido")
	}
	braveUserDataDir := filepath.Join(localAppData, "BraveSoftware", "Brave-Browser", "User Data")
	braveBin := `C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`

	u := launcher.New().
		Bin(braveBin).
		UserDataDir(braveUserDataDir).
		Headless(false).
		Leakless(true).
		MustLaunch()

	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("conectando Brave: %w", err)
	}

	page := browser.MustPage("https://chat.deepseek.com/")
	page.MustWaitLoad()
	time.Sleep(2 * time.Second)

	const responseSelector = "div.ds-assistant-message-main-content"
	n := 0
	if elems, e := page.Elements(responseSelector); e == nil {
		n = len(elems)
	}

	return &SesionDeepSeek{
		browser:       browser,
		page:          page,
		numRespuestas: n,
	}, nil
}

func (s *SesionDeepSeek) Cerrar() {
	if s.browser != nil {
		_ = s.browser.Close()
	}
}

// Enviar inyecta `prompt` en el textarea del chat actual, lo envía y espera
// a que aparezca una NUEVA respuesta del asistente (numRespuestas+1).
// Se puede llamar N veces seguidas: el chat crece, el contador avanza.
func (s *SesionDeepSeek) Enviar(prompt string) (respuesta string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic en browser: %v", r)
		}
	}()

	prompt = strings.TrimSpace(ColapsarAUnaLinea(prompt))
	if prompt == "" {
		return "", fmt.Errorf("prompt vacío")
	}

	const editorSelector = `textarea[placeholder="Message DeepSeek"]`
	const responseSelector = "div.ds-assistant-message-main-content"
	const sendBtnSelector = `div[role="button"].ds-button--primary.ds-button--circle:not(.ds-button--disabled)`

	page := s.page

	// Sincronizar contador (por si el chat ya tenía respuestas al abrirlo).
	if elems, e := page.Elements(responseSelector); e == nil && len(elems) > s.numRespuestas {
		s.numRespuestas = len(elems)
	}

	editor := page.MustElement(editorSelector)
	editor.MustWaitVisible()
	editor.MustClick()
	editor.MustFocus()
	time.Sleep(300 * time.Millisecond)

	// Quitar maxlength si existiera (bloquea prompts grandes).
	if maxLen, e := editor.Attribute("maxlength"); e == nil && maxLen != nil && *maxLen != "" {
		log.Printf("⚠️  textarea tiene maxlength=%s, quitando", *maxLen)
		_, _ = editor.Eval(`function() {
			this.removeAttribute('maxlength');
			return true;
		}`)
	}

	log.Printf("Prompt one-line: %d chars (respuestas previas: %d)",
		len(prompt), s.numRespuestas)

	// Limpiar el editor antes de inyectar (por si quedó algo del envío anterior).
	_, _ = editor.Eval(`function() {
		const el = this;
		const desc = Object.getOwnPropertyDescriptor(
			window.HTMLTextAreaElement.prototype, 'value'
		);
		desc.set.call(el, '');
		el.dispatchEvent(new Event('input', { bubbles: true }));
	}`)
	time.Sleep(200 * time.Millisecond)
	editor.MustFocus()

	if !inyectarPrompt(page, editor, prompt) {
		return "", fmt.Errorf("no pude inyectar el prompt en el textarea")
	}

	// Enviar
	enviado := false
	for i := 0; i < 20; i++ {
		if btn, e := page.Element(sendBtnSelector); e == nil && btn != nil {
			if err := btn.Click("left", 1); err == nil {
				enviado = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !enviado {
		if err := page.Keyboard.Press(input.Enter); err != nil {
			return "", fmt.Errorf("no pude enviar el prompt: %w", err)
		}
	}

	// Confirmar vaciado del textarea
	for i := 0; i < 10; i++ {
		if v, _ := editor.Property("value"); v.Str() == "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Esperar NUEVA respuesta (la posición numRespuestas+1 del chat).
	objetivo := s.numRespuestas + 1
	var textoRespuesta string
	textoAnterior := ""
	estable := 0
	const esperaSegundos = 1
	const requeridasEstables = 3

	for i := 0; i < 600; i++ {
		elems, e := page.Elements(responseSelector)
		if e != nil || len(elems) < objetivo {
			time.Sleep(esperaSegundos * time.Second)
			continue
		}
		ultima := elems[len(elems)-1]
		texto, e := ultima.Text()
		if e != nil || texto == "" {
			time.Sleep(esperaSegundos * time.Second)
			continue
		}
		if texto == textoAnterior {
			estable++
			if estable >= requeridasEstables {
				textoRespuesta = texto
				break
			}
		} else {
			estable = 0
		}
		textoAnterior = texto
		time.Sleep(esperaSegundos * time.Second)
	}

	if textoRespuesta == "" {
		return "", fmt.Errorf("timeout esperando respuesta de DeepSeek")
	}

	// Avanzar el contador para el próximo Enviar.
	if elems, e := page.Elements(responseSelector); e == nil {
		s.numRespuestas = len(elems)
	} else {
		s.numRespuestas = objetivo
	}

	if esRateLimitRespuesta(textoRespuesta) {
		log.Printf("⏸️  rate limit detectado: %q", strings.TrimSpace(textoRespuesta))
		return "", errRateLimit
	}

	log.Printf("Respuesta recibida (%d chars, mensajes asistente=%d)",
		len(textoRespuesta), s.numRespuestas)
	return strings.TrimSpace(textoRespuesta), nil
}

// inyectarPrompt prueba 3 métodos con verificación de contenido.
func inyectarPrompt(page *rod.Page, editor *rod.Element, prompt string) bool {
	intentos := []struct {
		nombre string
		fn     func() error
	}{
		{"Page.InsertText (CDP paste)", func() error {
			return page.InsertText(prompt)
		}},
		{"Portapapeles + Ctrl+V", func() error {
			if err := setClipboardWindows(prompt); err != nil {
				return fmt.Errorf("set clipboard: %w", err)
			}
			time.Sleep(200 * time.Millisecond)
			editor.MustFocus()
			time.Sleep(100 * time.Millisecond)
			page.Keyboard.MustType(input.ControlLeft)
			time.Sleep(50 * time.Millisecond)
			page.Keyboard.MustType(input.KeyV)
			time.Sleep(50 * time.Millisecond)
			page.Keyboard.MustType(input.ControlLeft)
			return nil
		}},
		{"Eval con setter nativo", func() error {
			_, e := editor.Eval(`function(text) {
				const el = this;
				const desc = Object.getOwnPropertyDescriptor(
					window.HTMLTextAreaElement.prototype, 'value'
				);
				desc.set.call(el, text);
				el.dispatchEvent(new Event('input',  { bubbles: true }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
				el.focus();
				return el.value;
			}`, prompt)
			return e
		}},
	}

	for i, intento := range intentos {
		_, _ = editor.Eval(`function() {
			const el = this;
			const desc = Object.getOwnPropertyDescriptor(
				window.HTMLTextAreaElement.prototype, 'value'
			);
			desc.set.call(el, '');
			el.dispatchEvent(new Event('input', { bubbles: true }));
		}`)
		time.Sleep(200 * time.Millisecond)
		editor.MustFocus()

		log.Printf("Intento %d/%d: %s", i+1, len(intentos), intento.nombre)
		if e := intento.fn(); e != nil {
			log.Printf("  ⚠️  %s: %v", intento.nombre, e)
			continue
		}
		time.Sleep(800 * time.Millisecond)

		val, e := editor.Property("value")
		if e != nil {
			log.Printf("  ⚠️  %s: no pude leer value: %v", intento.nombre, e)
			continue
		}
		got := strings.TrimSpace(val.Str())
		if got == prompt {
			log.Printf("  ✅ inyectado correctamente (%d chars, método: %s)",
				len(got), intento.nombre)
			return true
		}
		log.Printf("  ⚠️  %s: textarea=%d chars, prompt=%d chars",
			intento.nombre, len(got), len(prompt))
	}
	return false
}

// ObtenerRespuestaDeepSeek: wrapper de un solo uso (para compatibilidad).
// Internamente crea una sesión de un solo prompt. Para pipelines multi-chunk
// usa NuevaSesionDeepSeek() + sesion.Enviar() directamente.
func ObtenerRespuestaDeepSeek(prompt string) (string, error) {
	sesion, err := NuevaSesionDeepSeek()
	if err != nil {
		return "", err
	}
	defer sesion.Cerrar()
	return sesion.Enviar(prompt)
}

func setClipboardWindows(text string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		"Set-Clipboard -Value ([Console]::In.ReadToEnd())")
	cmd.Stdin = strings.NewReader(text)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("powershell Set-Clipboard: %v: %s", err, stderr.String())
	}
	return nil
}