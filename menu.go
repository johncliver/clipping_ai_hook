package main

import (
	"fmt"

	"clipping_ia_hook/internal/ui"
)

func imprimirCabecera(source, outDir string) {
	fmt.Println()
	fmt.Println(ui.Col(ui.Cyan, "════════════════════════════════════════════════════════"))
	fmt.Println("  " + ui.B("✂️  Clipping IA Hook"))
	fmt.Println()
	fmt.Println("  " + ui.D("📂 Origen:  ") + ui.Col(ui.White, source))
	fmt.Println("  " + ui.D("📁 Salida:  ") + ui.Col(ui.White, outDir+"/"))
	fmt.Println("  " + ui.D("🔧 Texto:   ") + ui.Estado(SeguirTexto))
	fmt.Println(ui.Col(ui.Cyan, "════════════════════════════════════════════════════════"))
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
