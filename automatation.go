package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
)

// ObtenerRespuestaDeepSeek abre Brave, envía el prompt y devuelve la respuesta como string.
// Si prefieres API en vez de browser, cambia esto por un http.Post a la API de DeepSeek.
func ObtenerRespuestaDeepSeek(prompt string) (respuesta string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic en browser: %v", r)
		}
	}()

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
		Leakless(false).
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

	page.MustElement(editorSelector).MustWaitVisible()
	editor := page.MustElement(editorSelector)
	editor.MustClick().MustFocus()
	editor.MustInput(prompt)
	time.Sleep(500 * time.Millisecond)
	page.Keyboard.MustType(input.Enter)

	var textoRespuesta string
	textoAnterior := ""
	estable := 0
	const esperaSegundos = 1
	const requeridasEstables = 3

	for i := 0; i < 600; i++ { // ~10 min
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
