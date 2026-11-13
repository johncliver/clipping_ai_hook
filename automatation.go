package main

import (
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

func ObtenerRespuestaDeepSeek(prompt string) (respuesta string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic en browser: %v", r)
		}
	}()

	// Forzar one-line por si el caller no lo hizo
	prompt = strings.TrimSpace(ColapsarAUnaLinea(prompt))
	if prompt == "" {
		return "", fmt.Errorf("prompt vacío")
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return "", fmt.Errorf("LOCALAPPDATA no definido")
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
		return "", fmt.Errorf("conectando Brave: %w", err)
	}
	defer browser.Close()

	page := browser.MustPage("https://chat.deepseek.com/")
	page.MustWaitLoad()
	time.Sleep(2 * time.Second)

	editorSelector := `textarea[placeholder="Message DeepSeek"]`
	responseSelector := "div.ds-assistant-message-main-content"

	previas := 0
	if elems, e := page.Elements(responseSelector); e == nil {
		previas = len(elems)
	}

	editor := page.MustElement(editorSelector)
	editor.MustWaitVisible()
	editor.MustClick()
	editor.MustFocus()
	time.Sleep(300 * time.Millisecond)

	// Quitar maxlength si existiera (bloquea prompts grandes)
	if maxLen, e := editor.Attribute("maxlength"); e == nil && maxLen != nil && *maxLen != "" {
		log.Printf("⚠️  textarea tiene maxlength=%s, quitando", *maxLen)
		_, _ = editor.Eval(`function() {
			this.removeAttribute('maxlength');
			return true;
		}`)
	}

	log.Printf("Prompt one-line: %d chars", len(prompt))

	intentos := []struct {
		nombre string
		fn     func() error
	}{
		// 1) rod wrapper de CDP Input.insertText (firma correcta, devuelve error)
		{"Page.InsertText (CDP paste)", func() error {
			return page.InsertText(prompt)
		}},
		// 2) Portapapeles + Ctrl+V (Down/Press/Up para mantener Ctrl pulsado)
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
		// 3) Eval con setter nativo (fallback final)
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

	var inyectado bool
	var ultimoDiagnostico string
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
			ultimoDiagnostico = fmt.Sprintf("%s: %v", intento.nombre, e)
			log.Printf("  ⚠️  %s", ultimoDiagnostico)
			continue
		}
		time.Sleep(800 * time.Millisecond)

		val, e := editor.Property("value")
		if e != nil {
			ultimoDiagnostico = fmt.Sprintf("%s: no pude leer value: %v", intento.nombre, e)
			log.Printf("  ⚠️  %s", ultimoDiagnostico)
			continue
		}
		got := strings.TrimSpace(val.Str())

		if got == prompt {
			log.Printf("  ✅ inyectado correctamente (%d chars, método: %s)", len(got), intento.nombre)
			inyectado = true
			break
		}

		diff := firstDiffIndex(prompt, got)
		ultimoDiagnostico = fmt.Sprintf(
			"%s: textarea=%d chars, prompt=%d chars, primer diff en pos=%d",
			intento.nombre, len(got), len(prompt), diff)
		log.Printf("  ⚠️  %s", ultimoDiagnostico)

		if diff >= 0 && diff < 120 {
			ini := diff - 20
			if ini < 0 {
				ini = 0
			}
			fin := diff + 20
			if fin > len(got) {
				fin = len(got)
			}
			log.Printf("      contexto textarea: %q", got[ini:fin])
			finP := diff + 20
			if finP > len(prompt) {
				finP = len(prompt)
			}
			log.Printf("      contexto prompt:   %q", prompt[ini:finP])
		}
	}

	if !inyectado {
		return "", fmt.Errorf("no pude inyectar el prompt en el textarea (%s)", ultimoDiagnostico)
	}

	sendBtnSelector := `div[role="button"].ds-button--primary.ds-button--circle:not(.ds-button--disabled)`

	var enviado bool
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

	for i := 0; i < 10; i++ {
		if v, _ := editor.Property("value"); v.Str() == "" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	var textoRespuesta string
	textoAnterior := ""
	estable := 0
	const esperaSegundos = 1
	const requeridasEstables = 3

	for i := 0; i < 600; i++ {
		elems, e := page.Elements(responseSelector)
		if e != nil || len(elems) <= previas {
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

	log.Printf("Respuesta recibida (%d chars)", len(textoRespuesta))
	return strings.TrimSpace(textoRespuesta), nil
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

func firstDiffIndex(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}
