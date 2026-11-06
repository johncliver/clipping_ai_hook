package main

import (
	"fmt"
	"os"
	"path/filepath"

	"clipping_ia_hook/internal/ui"
)

func showStatus(outDir string) {
	fmt.Println(ui.Col(ui.Cyan, ui.Rule))
	files := []string{
		"source.txt", "video.mp4", "transcripcion.json",
		"caras.json", "texto.json",
		"prompt_deepseek.txt", "respuesta_deepseek.txt",
		"music_video_transcripcion.json", "music_video.ass", "music_video.mp4",
	}
	for _, f := range files {
		p := filepath.Join(outDir, f)
		if info, err := os.Stat(p); err == nil {
			fmt.Printf("  %s %-32s %s\n",
				ui.Col(ui.Green, "✅"),
				ui.Col(ui.White, f),
				ui.Col(ui.Gray, fmt.Sprintf("%.1f KB", float64(info.Size())/1024)))
		} else {
			fmt.Printf("  %s %-32s %s\n",
				ui.Col(ui.Red, "❌"),
				ui.Col(ui.White, f),
				ui.Col(ui.Gray, "(falta)"))
		}
	}
	clips, _ := filepath.Glob(filepath.Join(outDir, "clip_*.mp4"))
	fmt.Printf("  %s %s\n",
		ui.Col(ui.Cyan, "📹"),
		ui.Col(ui.White, fmt.Sprintf("%d clips generados", len(clips))))
	fmt.Printf("  %s %s\n",
		ui.Col(ui.Cyan, "🔧"),
		ui.Col(ui.White, "Seguir texto en crop: ")+ui.Estado(SeguirTexto))
	fmt.Println(ui.Col(ui.Cyan, ui.Rule))
}
