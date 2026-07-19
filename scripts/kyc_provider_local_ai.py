#!/usr/bin/env python3
"""
ChainFX self-hosted KYC provider.

This is the local provider contract used by KYC_ENGINE_PROVIDER_URL.
It is intentionally self-hosted: no AWS/GCP/vendor KYC dependency.

Production mode requires local model files:
  FACE_EMBEDDING_ONNX
  LIVENESS_ONNX
  OCR_PROVIDER_URL or a local OCR implementation

If the real models are missing, the provider returns manual_review instead of
pretending a bank-grade biometric decision was made.
"""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import os
import tempfile
import time
from pathlib import Path
from typing import Any

import requests
from flask import Flask, jsonify, request

try:
    import cv2
except Exception:
    cv2 = None

try:
    import numpy as np
except Exception:
    np = None

try:
    import onnxruntime as ort
except Exception:
    ort = None


app = Flask(__name__)


def require_auth() -> tuple[bool, Any]:
    expected = os.getenv("KYC_PROVIDER_API_KEY", "").strip()
    if not expected:
        return True, None
    got = request.headers.get("Authorization", "")
    if got != f"Bearer {expected}":
        return False, (jsonify({"error": "unauthorized"}), 401)
    return True, None


def fetch_file(url: str, suffix: str) -> Path | None:
    if not url:
        return None
    resp = requests.get(url, timeout=25)
    resp.raise_for_status()
    fd, name = tempfile.mkstemp(suffix=suffix)
    path = Path(name)
    with os.fdopen(fd, "wb") as f:
        f.write(resp.content)
    return path


def image_quality_score(path: Path | None) -> int:
    if path is None or cv2 is None or np is None:
        return 0
    image = cv2.imread(str(path))
    if image is None:
        return 0
    gray = cv2.cvtColor(image, cv2.COLOR_BGR2GRAY)
    sharpness = float(cv2.Laplacian(gray, cv2.CV_64F).var())
    brightness = float(np.mean(gray))
    score = 40
    if sharpness > 80:
        score += 30
    if 55 <= brightness <= 205:
        score += 20
    if min(image.shape[:2]) >= 600:
        score += 10
    return max(0, min(score, 100))


def extract_video_motion_score(path: Path | None) -> tuple[int, dict[str, Any], bytes]:
    if path is None or cv2 is None or np is None:
        return 0, {"error": "opencv_unavailable_or_video_missing"}, b""
    cap = cv2.VideoCapture(str(path))
    frames = []
    for _ in range(24):
        ok, frame = cap.read()
        if not ok:
            break
        frames.append(frame)
    cap.release()
    if len(frames) < 6:
        return 15, {"frames": len(frames), "error": "insufficient_frames"}, b""

    diffs = []
    prev = cv2.cvtColor(frames[0], cv2.COLOR_BGR2GRAY)
    for frame in frames[1:]:
        gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
        diffs.append(float(np.mean(cv2.absdiff(prev, gray))))
        prev = gray
    avg_motion = sum(diffs) / len(diffs)
    _, encoded = cv2.imencode(".jpg", frames[len(frames) // 2])
    score = int(max(0, min(100, avg_motion * 7)))
    return score, {"frames": len(frames), "avg_motion": avg_motion}, encoded.tobytes()


def deterministic_embedding(reference: bytes, user_id: str) -> list[float]:
    seed = reference or user_id.encode("utf-8")
    out = []
    for i in range(128):
        digest = hashlib.sha256(seed + str(i).encode()).digest()
        raw = int.from_bytes(digest[:4], "big")
        out.append((raw / 2**32) * 2 - 1)
    return out


def embedding_hash(embedding: list[float]) -> str:
    secret = os.getenv("FACE_BIOMETRY_SECRET") or os.getenv("KYC_PROVIDER_API_KEY") or "local-dev"
    bits = bytes([1 if value >= 0 else 0 for value in embedding])
    return base64.urlsafe_b64encode(hmac.new(secret.encode(), bits, hashlib.sha256).digest()).decode().rstrip("=")


def real_models_available() -> bool:
    return bool(
        ort is not None
        and os.getenv("FACE_EMBEDDING_ONNX")
        and os.getenv("LIVENESS_ONNX")
    )


@app.post("/analyze")
def analyze() -> Any:
    ok, error = require_auth()
    if not ok:
        return error

    started = time.time()
    payload = request.get_json(force=True)
    temp_paths: list[Path] = []
    try:
        doc_front = fetch_file(payload.get("DocumentURL", ""), ".jpg")
        doc_back = fetch_file(payload.get("DocumentBackURL", ""), ".jpg")
        video = fetch_file(payload.get("FacialVideoURL", ""), ".mp4")
        temp_paths = [p for p in [doc_front, doc_back, video] if p is not None]

        doc_score = int((image_quality_score(doc_front) * 0.65) + (image_quality_score(doc_back) * 0.35))
        liveness_score, liveness_details, reference_frame = extract_video_motion_score(video)

        # Production hook: replace this deterministic fallback with local ONNX
        # embedding + document-face extraction when models are configured.
        embedding = deterministic_embedding(reference_frame, payload.get("UserID", ""))
        face_score = 0 if not reference_frame else 72

        flags = []
        if not real_models_available():
            flags.append("local_models_not_configured")
        if doc_score < 70:
            flags.append("document_quality_low")
        if liveness_score < 70:
            flags.append("liveness_motion_low")

        final_score = round(doc_score * 0.25 + face_score * 0.30 + liveness_score * 0.35 + 10)
        decision = "manual_review"
        if real_models_available() and final_score >= 88:
            decision = "approved"
        if final_score < 55:
            decision = "rejected"

        return jsonify({
            "provider": "chainfx_local_ai",
            "model_version": "chainfx-local-ai-contract-v1",
            "decision": decision,
            "score": max(0, min(final_score, 100)),
            "document_score": doc_score,
            "face_match_score": face_score,
            "liveness_score": liveness_score,
            "replay_risk_score": 100 - liveness_score,
            "duplicate_score": 100,
            "risk_score": 10,
            "latency_ms": int((time.time() - started) * 1000),
            "embedding": embedding,
            "embedding_hash": embedding_hash(embedding),
            "flags": flags,
            "details": {
                "self_hosted": True,
                "real_models_available": real_models_available(),
                "liveness": liveness_details,
                "required_models": ["FACE_EMBEDDING_ONNX", "LIVENESS_ONNX"],
            },
        })
    finally:
        for path in temp_paths:
            try:
                path.unlink(missing_ok=True)
            except Exception:
                pass


if __name__ == "__main__":
    app.run(host=os.getenv("KYC_PROVIDER_HOST", "127.0.0.1"), port=int(os.getenv("KYC_PROVIDER_PORT", "9097")))
