#!/usr/bin/env python3
"""
Detecta regiones de texto en un video. Devuelve bloques con (t, x, y, w, h)
en coords relativas 0-1. Prueba en orden:
  1. pytesseract (OCR completo)
  2. EAST (si frozen_east_text_detection.pb existe al lado del script)
  3. Fallback: bordes + componentes (siempre funciona)
Salida JSON por stdout, logs por stderr.
"""
import sys, json, os
import cv2
import numpy as np


def _merge_boxes(boxes, w, h):
    """Agrupa detecciones cercanas en líneas horizontales."""
    if not boxes:
        return []
    boxes.sort(key=lambda b: (b[1], b[0]))
    merged = []
    for b in boxes:
        placed = False
        for m in merged:
            # mismo "renglón": solapamiento vertical
            if not (b[3] < m[1] or b[1] > m[3]):
                gap = b[0] - m[2]
                if 0 <= gap < 80:
                    m[2] = max(m[2], b[2])
                    m[1] = min(m[1], b[1])
                    m[3] = max(m[3], b[3])
                    m[4] = max(m[4], b[4])
                    placed = True
                    break
        if not placed:
            merged.append(list(b))
    out = []
    for x1, y1, x2, y2, conf in merged:
        ww, hh = x2 - x1, y2 - y1
        if ww < 40 or hh < 12:
            continue
        out.append({
            "x": round(x1 / w, 4),
            "y": round(y1 / h, 4),
            "w": round(ww / w, 4),
            "h": round(hh / h, 4),
            "conf": int(conf),
        })
    return out


def _make_detector():
    # 1) pytesseract
    try:
        import pytesseract
        try:
            pytesseract.get_tesseract_version()
        except Exception:
            raise RuntimeError("tesseract no encontrado")

        def detect(frame_bgr, w, h):
            rgb = cv2.cvtColor(frame_bgr, cv2.COLOR_BGR2RGB)
            data = pytesseract.image_to_data(
                rgb, output_type=pytesseract.Output.DICT,
                config="--psm 11")
            boxes = []
            n = len(data["text"])
            for i in range(n):
                txt = (data["text"][i] or "").strip()
                try:
                    conf = int(data["conf"][i])
                except Exception:
                    continue
                if conf < 40 or len(txt) < 2:
                    continue
                x, y = data["left"][i], data["top"][i]
                bw, bh = data["width"][i], data["height"][i]
                if bw < 20 or bh < 12:
                    continue
                boxes.append([x, y, x + bw, y + bh, conf])
            return _merge_boxes(boxes, w, h)
        return detect, "pytesseract"
    except Exception:
        pass

    # 2) EAST
    try:
        model = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                             "frozen_east_text_detection.pb")
        if not os.path.exists(model):
            raise FileNotFoundError(model)
        net = cv2.dnn.readNet(model)

        def decode_east(scores, geo, W, H, min_conf):
            rects = []
            numRows, numCols = scores.shape[2:4]
            for y in range(numRows):
                sd = scores[0, 0, y]
                x0 = geo[0, 0, y]; x1 = geo[0, 1, y]
                x2 = geo[0, 2, y]; x3 = geo[0, 3, y]
                ang = geo[0, 4, y]
                for x in range(numCols):
                    if sd[x] < min_conf:
                        continue
                    ox, oy = x * 4.0, y * 4.0
                    a = ang[x]
                    cos, sin = np.cos(a), np.sin(a)
                    hh = x0[x] + x2[x]; ww = x1[x] + x3[x]
                    endX = int(ox + cos * x1[x] + sin * x2[x])
                    endY = int(oy - sin * x1[x] + cos * x2[x])
                    startX = int(endX - ww); startY = int(endY - hh)
                    rects.append((startX, startY, endX, endY))
            return rects

        def detect(frame_bgr, w, h):
            W = (w + 31) // 32 * 32
            H = (h + 31) // 32 * 32
            img = cv2.resize(frame_bgr, (W, H))
            blob = cv2.dnn.blobFromImage(img, 1.0, (W, H),
                (123.68, 116.78, 103.94), swapRB=True, crop=False)
            net.setInput(blob)
            scores, geo = net.forward(["feature_fusion/Conv_7/Sigmoid",
                                       "feature_fusion/concat_3"])
            rects = decode_east(scores, geo, W, H, 0.5)
            sx, sy = w / W, h / H
            out = []
            for (x1, y1, x2, y2) in rects:
                x1, y1, x2, y2 = int(x1*sx), int(y1*sy), int(x2*sx), int(y2*sy)
                if x2 - x1 < 20 or y2 - y1 < 12:
                    continue
                out.append([x1, y1, x2, y2, 90])
            return _merge_boxes(out, w, h)
        return detect, "east"
    except Exception:
        pass

    # 3) Fallback: bordes + morfología
    def detect(frame_bgr, w, h):
        gray = cv2.cvtColor(frame_bgr, cv2.COLOR_BGR2GRAY)
        edges = cv2.Canny(gray, 80, 200)
        kernel = cv2.getStructuringElement(cv2.MORPH_RECT, (15, 3))
        closed = cv2.morphologyEx(edges, cv2.MORPH_CLOSE, kernel)
        contours, _ = cv2.findContours(closed, cv2.RETR_EXTERNAL,
                                       cv2.CHAIN_APPROX_SIMPLE)
        out = []
        for c in contours:
            x, y, bw, bh = cv2.boundingRect(c)
            if bw < 60 or bh < 16:
                continue
            if bw / max(1.0, bh) < 1.5:
                continue
            area = bw * bh
            if area < 0.0008 * w * h:
                continue
            if area > 0.5 * w * h:
                continue
            out.append([x, y, x + bw, y + bh, 60])
        return _merge_boxes(out, w, h)
    return detect, "edges-fallback"


def detect_video(path, sample_interval=0.5):
    cap = cv2.VideoCapture(path)
    if not cap.isOpened():
        raise RuntimeError(f"no se pudo abrir {path}")
    fps = cap.get(cv2.CAP_PROP_FPS) or 30.0
    total = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
    width = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH))
    height = int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT))
    duration = total / fps if fps > 0 else 0

    detect_fn, backend = _make_detector()
    print(f"[text_detect] backend={backend} size={width}x{height} dur={duration:.1f}s",
          file=sys.stderr)

    samples = []
    t = 0.0
    while t < duration:
        cap.set(cv2.CAP_PROP_POS_FRAMES, max(0, int(t * fps)))
        ok, frame = cap.read()
        if not ok:
            break
        boxes = detect_fn(frame, width, height)
        if boxes:
            samples.append({"t": round(t, 2), "regions": boxes})
        t += sample_interval
    cap.release()
    return {"width": width, "height": height, "fps": fps,
            "duration": duration, "samples": samples}


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Uso: python text_detect.py <video>", file=sys.stderr)
        sys.exit(1)
    try:
        print(json.dumps(detect_video(sys.argv[1])))
    except Exception as e:
        import traceback
        traceback.print_exc()
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)