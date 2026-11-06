package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clipping_ia_hook/internal/ui"
)

func main() {
	outDir := "procesados"
	if err := os.MkdirAll(outDir, 0755); err != nil {
		ui.ErrLn("creando " + outDir + ": " + err.Error())
		return
	}

	sourcePath := filepath.Join(outDir, "source.txt")
	var source string
	if data, err := os.ReadFile(sourcePath); err == nil {
		source = strings.TrimSpace(string(data))
	}
	if source == "" {
		source = ui.Ask("Ruta al video o URL de YouTube")
		_ = os.WriteFile(sourcePath, []byte(source), 0644)
	}

	imprimirCabecera(source, outDir)

	for {
		imprimirMenu()
		opt := ui.Ask("Elige una opción")

		switch opt {
		case "1":
			doTranscribe(source, outDir)
		case "2":
			if doFaces(source, outDir) {
				chainFrom(3, source, outDir)
			}
		case "3":
			if doText(source, outDir) {
				chainFrom(4, source, outDir)
			}
		case "4":
			if doPrompt(outDir) {
				chainFrom(5, source, outDir)
			}
		case "5":
			if doDeepSeek(outDir) {
				chainFrom(6, source, outDir)
			}
		case "6":
			doClips(source, outDir)
		case "7":
			if !doTranscribe(source, outDir) {
				break
			}
			if !doFaces(source, outDir) {
				break
			}
			if !doText(source, outDir) {
				break
			}
			if !doPrompt(outDir) {
				break
			}
			if !doDeepSeek(outDir) {
				break
			}
			doClips(source, outDir)
		case "8":
			showStatus(outDir)
		case "9":
			source = ui.Ask("Nueva ruta/URL")
			_ = os.WriteFile(sourcePath, []byte(source), 0644)
			ui.OKLn("Origen actualizado → " + source)
		case "10":
			SeguirTexto = !SeguirTexto
			if SeguirTexto {
				ui.OKLn("Crop ahora SIGUE texto en pantalla (además de caras)")
			} else {
				fmt.Println(ui.Col(ui.Gray, "  ⏹️  Crop ahora solo sigue caras (no persigue texto)"))
			}
		case "11":
			doMusicVideo(outDir)
		case "12":
			doVertical(outDir)
		case "13":
			fmt.Println()
			fmt.Println(ui.Col(ui.Cyan, "  👋  Adiós"))
			fmt.Println()
			return
		default:
			ui.ErrLn("Opción inválida: " + opt)
		}
	}
}
