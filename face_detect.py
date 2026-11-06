#!/usr/bin/env python3
"""
Detecta TODAS las caras, las trackea por ID y estima quién está hablando
por movimiento de la zona de la boca. Prueba en orden:
  1. mediapipe mp.solutions  (<0.10)
  2. mediapipe tasks API     (>=0.10)
  3. Haar cascade            (bundled con opencv-python, siempre funciona)

Salida JSON a stdout; logs/debug a stderr.
"""
import sys, json, os
import cv2
import numpy as np


def _make_detector():
    # 1) mediapipe old API (mp.solutions)
    try:
        import mediapipe as mp
        if hasattr(mp, "solutions") and hasattr(mp.solutions, "face_detection"):
            fd = mp.solutions.face_detection.FaceDetection(
                model_selection=1, min_detection_confidence=0.5)
            def detect(frame_bgr, w, h):
                rgb = cv2.cvtColor(frame_bgr, cv2.COLOR_BGR2RGB)
                res = fd.process(rgb)
                out = []
                if res.detections:
                    for d in res.detections:
                        b = d.location_data.relative_bounding_box
                        out.append([int(b.xmin * w), int(b.ymin * h),
                                    int((b.xmin + b.width) * w),
                                    int((b.ymin + b.height) * h)])
                return out
            return detect, "mediapipe-solutions"
    except Exception:
        pass

    # 2) mediapipe tasks API (>=0.10)
    try:
        import mediapipe as mp
        from mediapipe.tasks import python as mp_py
        from mediapipe.tasks.python import vision as mp_vis
        model_path = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                                  "blaze_face_short_range.tflite")
        if not os.path.exists(model_path):
            import urllib.request
            url = ("https://storage.googleapis.com/mediapipe-models/face_detector/"
                   "blaze_face_short_range/float16/latest/blaze_face_short_range.tflite")
            urllib.request.urlretrieve(url, model_path)
        base = mp_py.BaseOptions(model_asset_path=model_path)
        opts = mp_vis.FaceDetectorOptions(
            base_options=base, min_detection_confidence=0.5)
        det = mp_vis.FaceDetector.create_from_options(opts)

        def detect(frame_bgr, w, h):
            rgb = cv2.cvtColor(frame_bgr, cv2.COLOR_BGR2RGB)
            mp_img = mp.Image(image_format=mp.ImageFormat.SRGB, data=rgb)
            res = det.detect(mp_img)
            return [[d.bounding_box.origin_x, d.bounding_box.origin_y,
                     d.bounding_box.origin_x + d.bounding_box.width,
                     d.bounding_box.origin_y + d.bounding_box.height]
                    for d in res.detections]
        return detect, "mediapipe-tasks"
    except Exception:
        pass

    # 3) Haar cascade
    cascade = cv2.CascadeClassifier(
        cv2.data.haarcascades + "haarcascade_frontalface_default.xml")

    def detect(frame_bgr, w, h):
        gray = cv2.cvtColor(frame_bgr, cv2.COLOR_BGR2GRAY)
        faces = cascade.detectMultiScale(
            gray, scaleFactor=1.1, minNeighbors=5, minSize=(40, 40))
        return [[int(x), int(y), int(x + fw), int(y + fh)]
                for (x, y, fw, fh) in faces]
    return detect, "haar"


def _iou(a, b):
    ax1, ay1, ax2, ay2 = a
    bx1, by1, bx2, by2 = b
    ix1, iy1 = max(ax1, bx1), max(ay1, by1)
    ix2, iy2 = min(ax2, bx2), min(ay2, by2)
    iw, ih = max(0, ix2 - ix1), max(0, iy2 - iy1)
    inter = iw * ih
    if inter == 0:
        return 0.0
    union = (ax2 - ax1) * (ay2 - ay1) + (bx2 - bx1) * (by2 - by1) - inter
    return inter / union if union > 0 else 0.0


def _mouth_motion(prev_gray, cur_gray, box):
    """Diferencia media en la parte inferior 40% del bbox (zona de boca)."""
    x1, y1, x2, y2 = box
    h = y2 - y1
    if h < 10 or (x2 - x1) < 10:
        return 0.0
    my1 = y1 + int(h * 0.58)
    my2 = y2
    a = prev_gray[my1:my2, x1:x2]
    b = cur_gray[my1:my2, x1:x2]
    if a.shape != b.shape or a.size == 0:
        return 0.0
    return float(cv2.absdiff(a, b).mean())


def detect(video_path, sample_interval=1.0, motion_window=0.35):
    cap = cv2.VideoCapture(video_path)
    if not cap.isOpened():
        raise RuntimeError(f"no se pudo abrir {video_path}")

    fps = cap.get(cv2.CAP_PROP_FPS) or 30.0
    total = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
    width = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH))
    height = int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT))
    duration = total / fps if fps > 0 else 0

    detect_fn, backend = _make_detector()
    print(f"[face_detect] backend={backend} size={width}x{height} dur={duration:.1f}s",
          file=sys.stderr)

    def frame_at(t):
        cap.set(cv2.CAP_PROP_POS_FRAMES, max(0, int(t * fps)))
        ok, fr = cap.read()
        return fr if ok else None

    next_id = 0
    tracks = {}
    samples = []

    t = 0.0
    while t < duration:
        frame = frame_at(t)
        if frame is None:
            break

        boxes = detect_fn(frame, width, height)

        # Tracking por IoU (asignar IDs estables)
        assigned = []
        for box in boxes:
            best_id, best_iou = None, 0.25
            for tid, tr in tracks.items():
                if t - tr["last_t"] > 3.0:
                    continue
                tb = [int((tr["cx"] - tr["w"] / 2) * width),
                      int((tr["cy"] - tr["h"] / 2) * height),
                      int((tr["cx"] + tr["w"] / 2) * width),
                      int((tr["cy"] + tr["h"] / 2) * height)]
                v = _iou(box, tb)
                if v > best_iou:
                    best_iou, best_id = v, tid
            if best_id is None:
                best_id = next_id
                next_id += 1
            cx = (box[0] + box[2]) / 2.0 / width
            cy = (box[1] + box[3]) / 2.0 / height
            fw = (box[2] - box[0]) / width
            fh = (box[3] - box[1]) / height
            tracks[best_id] = {"cx": cx, "cy": cy, "w": fw, "h": fh, "last_t": t}
            assigned.append({"id": best_id, "cx": cx, "cy": cy, "w": fw, "h": fh,
                             "box_px": box})

        # Hablante: comparar zona de boca entre t y t+motion_window
        speaking_id = None
        if assigned:
            frame2 = frame_at(min(t + motion_window, max(0, duration - 0.05)))
            if frame2 is not None:
                g1 = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
                g2 = cv2.cvtColor(frame2, cv2.COLOR_BGR2GRAY)
                motions = []
                for f in assigned:
                    m = _mouth_motion(g1, g2, f["box_px"])
                    f["mouth_motion"] = round(m, 2)
                    motions.append(m)

                if motions:
                    max_m = max(motions)
                    if len(motions) == 1:
                        if max_m > 1.5:
                            speaking_id = assigned[0]["id"]
                    else:
                        srt = sorted(motions, reverse=True)
                        second = srt[1]
                        # hablante claro: max alto y 2.5x el segundo,
                        # o el segundo está prácticamente quieto
                        if max_m > 1.5 and (second < 0.8 or max_m > second * 2.5):
                            speaking_id = assigned[motions.index(max_m)]["id"]

        for f in assigned:
            f.pop("box_px", None)

        samples.append({
            "t": round(t, 2),
            "faces": assigned,
            "speaking_id": speaking_id,
        })
        t += sample_interval

    cap.release()
    return {
        "width": width, "height": height,
        "fps": fps, "duration": duration,
        "samples": samples,
    }


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Uso: python face_detect.py <video>", file=sys.stderr)
        sys.exit(1)
    try:
        print(json.dumps(detect(sys.argv[1])))
    except Exception as e:
        import traceback
        traceback.print_exc()
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)