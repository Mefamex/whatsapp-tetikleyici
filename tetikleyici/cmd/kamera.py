#!/usr/bin/env python3
"""kamera.py — Bilgisayar kamerasından tek kare çek, mefamex'e gönder.

Çift kapı kilidi:
  1. `tetikleyici/cmd/yetki.py` — yalnız mefamex çalıştırabilir.
  2. `tetikleyici.yaml` → `kamera_izni: true` olmalı (varsayılan false).

Kamera mahremiyeti gereği varsayılan KAPALI. Açmak bilinçli tercihtir.

Akış: ffmpeg (v4l2) → JPEG → köprü POST /send-media → stdout'a kısa onay.
ÇIKIŞ SÖZLEŞMESİ: stdout DOLU olmak zorunda (bkz. gorsel_pipeline.py).
"""

from __future__ import annotations

import base64
import json
import os
import subprocess
import sys
import tempfile
import urllib.request
from datetime import datetime
from pathlib import Path


BRIDGE_BASE = os.getenv("BRIDGE_BASE", "http://127.0.0.1:31415")
KAMERA_CIHAZI = os.getenv("KAMERA_CIHAZI", "/dev/video0")
KAMERA_ALICI = os.getenv("KAMERA_ALICI", "905551234567")
KAMERA_TIMEOUT = int(os.getenv("KAMERA_TIMEOUT", "20"))


def log(mesaj: str) -> None:
    print(f"[kamera] {mesaj}", file=sys.stderr, flush=True)


def cek(hedef: Path, cihaz: str = KAMERA_CIHAZI) -> tuple[bool, str]:
    """Tek kare JPEG çek. (başarı, hata-mesajı)."""
    if not Path(cihaz).exists():
        return False, f"kamera cihazı yok: {cihaz}"

    cmd = [
        "ffmpeg",
        "-y",
        "-loglevel",
        "error",
        "-f",
        "v4l2",
        "-i",
        cihaz,
        "-frames:v",
        "1",
        str(hedef),
    ]
    try:
        proc = subprocess.run(
            cmd, timeout=KAMERA_TIMEOUT, capture_output=True, text=True
        )
    except FileNotFoundError:
        return False, "ffmpeg bulunamadı"
    except subprocess.TimeoutExpired:
        return False, f"kamera {KAMERA_TIMEOUT} sn'de cevap vermedi"

    if proc.returncode != 0:
        return False, f"ffmpeg çıkış {proc.returncode}: {proc.stderr.strip()[:200]}"
    if not hedef.exists() or hedef.stat().st_size == 0:
        return False, "JPEG oluşmadı (boş dosya)"
    return True, ""


def gonder(
    jpeg_yolu: Path, alici: str = KAMERA_ALICI, bridge: str = BRIDGE_BASE
) -> tuple[bool, str]:
    """JPEG'i köprü üzerinden gönder. (başarı, hata-mesajı)."""
    try:
        ham = jpeg_yolu.read_bytes()
    except OSError as e:
        return False, f"dosya okunamadı: {e}"

    govde = json.dumps(
        {
            "to": alici,
            "media_base64": base64.b64encode(ham).decode("ascii"),
            "mime_type": "image/jpeg",
            "caption": f"📷 kamera — {datetime.now().strftime('%H:%M:%S')}",
            "file_name": f"kamera-{datetime.now().strftime('%Y%m%d_%H%M%S')}.jpg",
        }
    ).encode("utf-8")

    istek = urllib.request.Request(
        f"{bridge.rstrip('/')}/send-media",
        data=govde,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(istek, timeout=30) as yanit:
            cevap = json.loads(yanit.read().decode("utf-8"))
    except Exception as e:  # noqa: BLE001 — kullanıcıya düz metin gider
        return False, f"köprüye ulaşılamadı: {e}"

    if not cevap.get("ok", False):
        return False, f"köprü reddetti: {str(cevap)[:200]}"
    return True, ""


def ana() -> str:
    """Çek + gönder, sonucu düz metin döndür (stdout sözleşmesi)."""
    with tempfile.NamedTemporaryFile(suffix=".jpg", delete=False) as tmp:
        jpeg_yolu = Path(tmp.name)

    try:
        ok, hata = cek(jpeg_yolu)
        if not ok:
            log(f"çekim hatası: {hata}")
            return f"Kameradan fotoğraf alınamadı ({hata}). Başka cihaz için KAMERA_CIHAZI ortam değişkeni."

        boyut_kb = jpeg_yolu.stat().st_size // 1024
        log(f"çekildi: {boyut_kb} KB")

        ok, hata = gonder(jpeg_yolu)
        if not ok:
            log(f"gönderim hatası: {hata}")
            return f"Fotoğraf çekildi ({boyut_kb} KB) ama gönderilemedi ({hata})."

        return f"Kamera fotoğrafı gönderildi ({boyut_kb} KB). 📷"
    finally:
        try:
            jpeg_yolu.unlink()
        except OSError:
            pass


if __name__ == "__main__":
    # Test: python3 kamera.py --cek-sadece  → yalnız çek, gönderme
    if len(sys.argv) > 1 and sys.argv[1] == "--cek-sadece":
        with tempfile.NamedTemporaryFile(suffix=".jpg", delete=False) as tmp:
            yol = Path(tmp.name)
        ok, hata = cek(yol)
        print(
            f"çekim: {ok} {hata} ({yol.stat().st_size} bayt)"
            if ok
            else f"çekim HATA: {hata}"
        )
    else:
        print(ana())
