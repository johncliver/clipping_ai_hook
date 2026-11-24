package main

import (
	"fmt"
	"path/filepath"

	"clipping_ia_hook/internal/ui"
)

func imprimirCabecera(source, outDir string) {
	fmt.Println()
	fmt.Println(ui.Col(ui.Cyan, "════════════════════════════════════════════════════════"))
	fmt.Println("  " + ui.B("✂️  Clipping IA Hook"))
	fmt.Println()
	fmt.Println("  " + ui.D("📂 Origen:  ") + ui.Col(ui.White, source))
	fmt.Println("  " + ui.D("📁 Salida:  ") + ui.Col(ui.White, outDir+"/"))
	fmt.Println("  " + ui.D("📱 Plataforma: ") + ui.Col(ui.White, plataformaActual.Nombre()))
	fmt.Println("  " + ui.D("🔧 Seguir texto: ") + ui.Estado(SeguirTexto))
	fmt.Println("  " + ui.D("🎬 Crop híbrido: ") + ui.Estado(CropHibrido))
	fmt.Println("  " + ui.D("🖼️  End card:") + " " + estadoEndCard())
	fmt.Println("  " + ui.D("🎞️  End video:") + " " + estadoEndVideo())
	fmt.Println("  " + ui.D("📝 Subtítulos:") + " " + estadoSubtitulos())
	fmt.Println(ui.Col(ui.Cyan, "════════════════════════════════════════════════════════"))
}

func estadoEndCard() string {
	if EndCardImagen == "" {
		return ui.Col(ui.Gray, "OFF")
	}
	return ui.Col(ui.Green+ui.Bold, fmt.Sprintf("ON · %.1fs · %s",
		EndCardDuracion, filepath.Base(EndCardImagen)))
}

func estadoSubtitulos() string {
	icono := "⬇️"
	switch SubsPosicion {
	case "top":
		icono = "⬆️"
	case "center":
		icono = "⏺️"
	}
	return fmt.Sprintf("%s %s · %dpx · %dpt",
		icono, SubsPosicion, SubsMargenV, SubsTamaño)
}

func imprimirMenu() {
	fmt.Println()
	fmt.Println(ui.Col(ui.Cyan, ui.Rule))

	filaAccion("1", "Transcribir", "transcripcion.json")
	filaAccion("2", "Caras", "caras.json")
	filaAccion("3", "Texto en pantalla", "texto.json")
	filaAccion("4", "Prompt", "prompt_deepseek.txt")
	filaAccion("5", "DeepSeek", "respuesta_deepseek.txt")
	filaAccion("6", "Generar clips", "clip_XX.mp4")

	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, fmt.Sprintf("%2s", "7")),
		ui.Col(ui.Green, "Pipeline completo  (1→2→3→4→5→6)"))

	fmt.Println(ui.Col(ui.Gray, ui.Rule))

	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, fmt.Sprintf("%2s", "8")),
		ui.Col(ui.White, "Ver estado"))
	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, fmt.Sprintf("%2s", "9")),
		ui.Col(ui.White, "Cambiar origen"))
	fmt.Printf("  %s)  %s  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "10"),
		ui.Col(ui.White, "Seguir texto en crop"),
		ui.Estado(SeguirTexto))
	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "11"),
		ui.Col(ui.Green, "Crear video con música/audio"))
	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "12"),
		ui.Col(ui.Green, "Horizontal → Vertical (1080x1920)"))
	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "13"),
		ui.Col(ui.Green, "Marca de agua a clips/videos"))
	fmt.Printf("  %s)  %s  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "14"),
		ui.Col(ui.White, "End card en clips"),
		ui.Estado(EndCardImagen != ""))
	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "15"),
		ui.Col(ui.Green, "Añadir end card a clips existentes"))
	fmt.Printf("  %s)  %s  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "16"),
		ui.Col(ui.White, "Crop híbrido (cara/blur)"),
		ui.Estado(CropHibrido))
	fmt.Printf("  %s)  %s  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "17"),
		ui.Col(ui.White, "Posición de subtítulos"),
		ui.Estado(SubsPosicion != "bottom"))
	fmt.Printf("  %s)  %s %s\n",
		ui.Col(ui.Yellow+ui.Bold, "18"),
		ui.Col(ui.White, "Tamaño crop horizontal (blur)"),
		ui.Col(ui.Gray, CropHorizontalPreset))
	fmt.Printf("  %s)  %s %s\n",
		ui.Col(ui.Yellow+ui.Bold, "19"),
		ui.Col(ui.White, "Tamaño de salida (vertical)"),
		ui.Col(ui.Gray, TamanoVerticalPreset))
	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "20"),
		ui.Col(ui.Green, "Subtítulos .ass desde transcripción (color karaoke configurable)"))
	fmt.Printf("  %s)  %s  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "21"),
		ui.Col(ui.White, "Plataforma de destino"),
		ui.Col(ui.Gray, plataformaActual.Nombre()))
	fmt.Printf("  %s)  %s  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "22"),
		ui.Col(ui.White, "End video en clips"),
		ui.Estado(EndVideoPath != ""))
	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "23"),
		ui.Col(ui.Green, "Añadir end video a clips existentes"))
	fmt.Printf("  %s)  %s\n",
		ui.Col(ui.Yellow+ui.Bold, "24"),
		ui.Col(ui.Red, "Salir"))
	fmt.Println(ui.Col(ui.Cyan, ui.Rule))
}

func filaAccion(num, accion, archivo string) {
	fmt.Printf("  %s)  %s %s %s\n",
		ui.Col(ui.Yellow+ui.Bold, fmt.Sprintf("%2s", num)),
		ui.Col(ui.White, fmt.Sprintf("%-22s", accion)),
		ui.Col(ui.Dim, "→"),
		ui.Col(ui.Gray, archivo))
}

// configurarPlataforma cambia el tope duro de duración por clip.
func configurarPlataforma() {
	fmt.Println()
	fmt.Println(ui.Col(ui.Cyan, "📱 Plataforma de destino"))
	fmt.Println(ui.Col(ui.Gray, "  Afecta al tope duro de duración por clip y al rango ideal."))
	fmt.Println()
	for i, p := range []Plataforma{PlataformaYouTube, PlataformaReels, PlataformaLarga} {
		marca := " "
		if p == plataformaActual {
			marca = "●"
		}
		fmt.Printf("  %s %d) %-24s  (máx %.0fs, ideal 30-%.0fs)\n",
			marca, i+1, p.Nombre(), p.MaxSeg(), p.IdealSeg())
	}
	fmt.Println()
	op := ui.Ask("Elige 1-3 (vacío = mantener)")
	if op == "" {
		return
	}
	switch op {
	case "1":
		plataformaActual = PlataformaYouTube
	case "2":
		plataformaActual = PlataformaReels
	case "3":
		plataformaActual = PlataformaLarga
	default:
		ui.ErrLn("Opción inválida")
		return
	}
	ui.OKLn("Plataforma → " + plataformaActual.Nombre())
	ui.WarnLn("Aplica a prompts/clips que se generen a partir de ahora")
}
