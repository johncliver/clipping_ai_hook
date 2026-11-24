package main

import "math"

// Chunk es una ventana de trabajo para el modelo.
// El modelo SOLO debe proponer clips cuyo inicio_seg ∈ [InicioSeg, FinSeg].
// La transcripción/caras mostradas abarcan [InicioCtx, FinCtx] (con overlap
// extra para que el modelo entienda cómo termina un clip que cruza el borde).
type Chunk struct {
	InicioSeg float64
	FinSeg    float64
	InicioCtx float64
	FinCtx    float64
}

const (
	// Tamaño objetivo de cada chunk.
	// 300s = 5min. Para 1h → 12 chunks. Para 2h → 24 chunks.
	ChunkTamObjetivoSeg = 300.0

	// Tamaño mínimo de un chunk (no cortar antes de esto salvo al final del video).
	ChunkTamMinSeg = 180.0

	// Tamaño máximo duro (no permitir chunks gigantes aunque no haya frase natural).
	ChunkTamMaxSeg = 480.0

	// Contexto extra a cada lado del chunk visible al modelo.
	// NO es solape de "propuesta": solo de visualización.
	ChunkOverlapSeg = 45.0

	// Máximo de clips por chunk en el prompt (evita respuestas gigantes).
	MaxClipsPorChunk = 5
)

// ConstruirChunks divide la transcripción en ventanas cortando SIEMPRE en
// fronteras naturales: fin de frase (puntuación) o, en su defecto, fin de
// palabra. Nunca corta una palabra por la mitad.
func ConstruirChunks(t *Transcripcion, tamObjetivo, overlap float64) []Chunk {
	if t == nil || len(t.Segmentos) == 0 {
		return nil
	}
	durTotal := t.Duracion()
	if durTotal <= 0 {
		return nil
	}
	if tamObjetivo <= 0 {
		tamObjetivo = ChunkTamObjetivoSeg
	}
	if overlap < 0 {
		overlap = ChunkOverlapSeg
	}

	// Si todo cabe en un único chunk (con margen), sin cortes.
	if durTotal <= tamObjetivo*1.2 {
		return []Chunk{{
			InicioSeg: 0, FinSeg: durTotal,
			InicioCtx: 0, FinCtx: durTotal,
		}}
	}

	palabras := aplanarPalabras(t)
	var chunks []Chunk
	inicio := 0.0

	for inicio < durTotal-1.0 {
		objetivo := math.Min(inicio+tamObjetivo, durTotal)
		fin := buscarFinNatural(palabras, inicio, objetivo, durTotal)

		if fin <= inicio+1.0 {
			fin = durTotal
		}

		ctxIni := math.Max(0, inicio-overlap)
		ctxFin := math.Min(durTotal, fin+overlap)

		chunks = append(chunks, Chunk{
			InicioSeg: inicio,
			FinSeg:    fin,
			InicioCtx: ctxIni,
			FinCtx:    ctxFin,
		})

		if fin >= durTotal-0.5 {
			break
		}
		inicio = fin
	}
	return chunks
}

// buscarFinNatural elige el mejor fin de chunk cerca de `objetivo`, dentro de
// [inicio+ChunkTamMinSeg, inicio+ChunkTamMaxSeg]. Prioridad:
//   1. fin de frase (puntuación)
//   2. cualquier fin de palabra (fallback: nunca cortar dentro de una palabra)
func buscarFinNatural(palabras []Palabra, inicio, objetivo, durTotal float64) float64 {
	if len(palabras) == 0 {
		return math.Min(objetivo, durTotal)
	}
	limMin := math.Min(inicio+ChunkTamMinSeg, durTotal)
	limMax := math.Min(inicio+ChunkTamMaxSeg, durTotal)
	if objetivo < limMin {
		objetivo = limMin
	}
	if objetivo > limMax {
		objetivo = limMax
	}

	mejorFrase := -1.0
	distFrase := math.Inf(1)
	mejorCualq := -1.0
	distCualq := math.Inf(1)

	for i := range palabras {
		p := &palabras[i]
		if p.Fin < limMin {
			continue
		}
		if p.Fin > limMax {
			break
		}
		d := math.Abs(p.Fin - objetivo)
		if d < distCualq {
			distCualq = d
			mejorCualq = p.Fin
		}
		if terminaFrase(p.Texto) && d < distFrase {
			distFrase = d
			mejorFrase = p.Fin
		}
	}

	if mejorFrase > 0 {
		return mejorFrase
	}
	if mejorCualq > 0 {
		return mejorCualq
	}
	return math.Min(objetivo, durTotal)
}
