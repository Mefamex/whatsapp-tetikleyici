#!/usr/bin/env python3
"""
voice_pipeline.py — WhatsApp sesli not → Türkçe transkript.

Akış:
    tetikleyici (sesli not geldi)
      → bu betik
        1. GET /media/{id}          : ses baytlarını indir
        2. ffmpeg ogg → wav 16k mono : faster-whisper bunu istiyor
        3. faster-whisper GPU        : whisper-large-v3-tr ile TR transkript
      → transkript stdout'a düşer
    tetikleyici stdout'u sohbete cevap olarak gönderir.

Kullanım:
    voice_pipeline.py <media_id> [--output text|json|srt]

Ortam değişkenleri:
    BRIDGE_BASE     varsayılan http://127.0.0.1:31415
    ASR_ROOT        varsayılan /mnt/DiskD/SOFTWARE/Github-repos/test_ASR
    WHISPER_MODEL   varsayılan large-v3-tr
    DEVICE          varsayılan cuda   (cpu yazılırsa GPU'suz çalışır)
    COMPUTE_TYPE    varsayılan int8_float16  (cpu için int8)

Not — GPU doluysa CPU'ya düşer:
    DEVICE=cuda iken model yüklenemezse (ör. `CUDA failed with error out of
    memory`) transkript bir kez de cpu/int8 ile denenir. Sesli not, GPU'nun
    boşalmasını beklemek yerine yavaş da olsa tamamlanır.

Not — LD_LIBRARY_PATH neden ayarlanıyor:
    faster-whisper CTranslate2'yi kullanır ve CTranslate2 CUDA 12'ye
    bağlıdır (libcublas.so.12). Sistemde CUDA 13 kurulu
    (libcublas.so.13), ama pip ile kurulan nvidia-* paketleri venv
    içinde doğru CUDA 12 kütüphanelerini taşır. Süreç, sistem
    kütüphanelerini önce bulup yanlış sürümü yükler. LD_LIBRARY_PATH
    ile venv içindekiler öne alınır. sudo gerekmez.
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request
from pathlib import Path

# ── Varsayılanlar ────────────────────────────────────────────────────────────
BRIDGE_BASE = os.getenv("BRIDGE_BASE", "http://127.0.0.1:31415")
ASR_ROOT = Path(os.getenv("ASR_ROOT", "/mnt/DiskD/SOFTWARE/Github-repos/test_ASR"))
WHISPER_MODEL = os.getenv("WHISPER_MODEL", "large-v3-tr")
DEVICE = os.getenv("DEVICE", "cuda")
COMPUTE_TYPE = os.getenv("COMPUTE_TYPE", "int8_float16")
TIMEOUT_S = int(os.getenv("VOICE_TIMEOUT", "600"))


def log(msg: str) -> None:
    """İlerleme bilgisi stderr'e gider; stdout yalnız transkript için kalır."""
    print(msg, file=sys.stderr, flush=True)


# ── Adım 1: medyayı indir ────────────────────────────────────────────────────
def download_media(media_id: str, bridge: str) -> bytes:
    url = f"{bridge.rstrip('/')}/media/{media_id}"
    try:
        with urllib.request.urlopen(url, timeout=60) as resp:
            return resp.read()
    except urllib.error.HTTPError as err:
        raise RuntimeError(f"köprü {url} → HTTP {err.code}: {err.reason}") from err
    except urllib.error.URLError as err:
        raise RuntimeError(f"köprüye ulaşılamadı ({url}): {err.reason}") from err


# ── Adım 2: ffmpeg ile wav'a çevir ───────────────────────────────────────────
def to_wav(data: bytes) -> str:
    if shutil.which("ffmpeg") is None:
        raise RuntimeError("ffmpeg bulunamadı (pacman -S ffmpeg)")

    with tempfile.TemporaryDirectory(prefix="ses-") as tmp:
        src = Path(tmp) / "gelen.ogg"
        dst = Path(tmp) / "gelen.wav"
        src.write_bytes(data)

        proc = subprocess.run(
            [
                "ffmpeg",
                "-y",
                "-i",
                str(src),
                "-ar",
                "16000",
                "-ac",
                "1",
                "-c:a",
                "pcm_s16le",
                str(dst),
            ],
            capture_output=True,
        )
        if proc.returncode != 0 or not dst.exists():
            tail = proc.stderr.decode("utf-8", "replace").strip().splitlines()[-3:]
            raise RuntimeError("ffmpeg dönüştüremedi: " + " | ".join(tail))

        # Geçici dizin kapanınca dosya silineceği için dışarıya taşı.
        keep = Path(tempfile.mkdtemp(prefix="ses-wav-")) / "gelen.wav"
        shutil.move(str(dst), str(keep))
        return str(keep)


# ── Adım 3: faster-whisper ile transkript ────────────────────────────────────
def transcribe(wav: str) -> list[dict]:
    """Transkripti üretir: önce birincil cihaz (varsayılan CUDA), olmazsa CPU.

    ÖLÇÜLEN HATA (2026-10-14 08:12 ve 08:38): GPU başka bir iş tarafından
    doluyken `WhisperModel(device="cuda")` şunu verdi:

        RuntimeError: CUDA failed with error out of memory

    Sonuç: sesli not tamamen düşüyordu (kullanıcıya "İş tamamlanamadı" gidiyor).
    GPU'nun boşalmasını beklemek yerine CPU'da int8 ile yeniden denemek sesli
    notu kurtarır — yavaş ama çalışır. Birincil cihaz zaten "cpu" ise (DEVICE
    ortam değişkeni) ikinci deneme yapılmaz; hata olduğu gibi yukarı çıkar.
    """
    try:
        return _whisper_calistir(wav, DEVICE, COMPUTE_TYPE)
    except RuntimeError as err:
        if DEVICE != "cuda":
            raise
        log(f"⚠️ GPU ile transkript olmadı ({err}); CPU/int8 ile deneniyor")
        return _whisper_calistir(wav, "cpu", "int8")


def _whisper_calistir(wav: str, device: str, compute_type: str) -> list[dict]:
    # faster-whisper'ı içe aktar, modeli bu süreçte bellekte tut, JSON'a yaz.
    # Böylece her mesajda modeli baştan yüklemiyoruz (~10 sn kazanç/mesaj).
    script = f"""
import json, sys
from faster_whisper import WhisperModel

model = WhisperModel({json.dumps(str(model_dir()))},
                     device={json.dumps(device)},
                     compute_type={json.dumps(compute_type)})
segments, info = model.transcribe(
    {json.dumps(wav)},
    language="tr",
    beam_size=5,
    vad_filter=True,
    word_timestamps=False,
)
out = [{{"start": s.start, "end": s.end, "text": s.text.strip()}} for s in segments]
json.dump(out, sys.stdout, ensure_ascii=False)

# VRAM temizliği: modeli sil ve CUDA önbelleğini serbest bırak
try:
    del model
    import torch
    if torch.cuda.is_available():
        torch.cuda.empty_cache()
except Exception:
    pass
"""
    env = asr_env()
    proc = subprocess.run(
        [venv_python(), "-c", script],
        capture_output=True,
        text=True,
        timeout=TIMEOUT_S,
        env=env,
    )
    if proc.returncode != 0:
        tail = proc.stderr.strip().splitlines()[-4:]
        raise RuntimeError("transkripsiyon başarısız: " + " | ".join(tail))
    try:
        return json.loads(proc.stdout or "[]")
    except json.JSONDecodeError as err:
        raise RuntimeError(f"transkript JSON okunamadı: {err}") from err


def venv_python() -> str:
    py = ASR_ROOT / "venv" / "bin" / "python"
    if not py.exists():
        raise RuntimeError(f"ASR venv bulunamadı: {py} (ASR_ROOT ayarla)")
    return str(py)


def model_dir() -> Path:
    """WHISPER_MODEL adını diskteki model dizinine çevirir.

    İki adlandırma var: kullanıcıya görünen `large-v3-tr` (transcribe.py'de
    seçenek adı) ve diskteki `whisper-large-v3-tr` (indirilen klasör).
    İkisini de kabul ediyoruz; ayrıca model klasörü değil dosya yolu
    verilmişse olduğu gibi kullanılır.
    """
    models = ASR_ROOT / "models"
    for candidate in (models / WHISPER_MODEL, models / f"whisper-{WHISPER_MODEL}"):
        if candidate.is_dir():
            return candidate
    if Path(WHISPER_MODEL).is_dir():
        return Path(WHISPER_MODEL)
    raise RuntimeError(
        f"model bulunamadı: {models / WHISPER_MODEL} "
        f"(ya da whisper-{WHISPER_MODEL}) — WHISPER_MODEL ayarla"
    )


def asr_env() -> dict:
    """Alt süreç ortamı: venv'in CUDA kütüphanelerini öne al."""
    env = dict(os.environ)
    site = ASR_ROOT / "venv" / "lib"
    libs = sorted(site.glob("python*/site-packages/nvidia/*/lib"))
    if libs:
        joined = ":".join(str(p) for p in libs)
        prev = env.get("LD_LIBRARY_PATH", "")
        env["LD_LIBRARY_PATH"] = f"{joined}:{prev}" if prev else joined
    return env


# ── Biçimlendirme ────────────────────────────────────────────────────────────
def stamp(seconds: float) -> str:
    """Saniyeyi SRT zaman damgasına çevirir: 00:00:25,000"""
    total_ms = max(0, int(round(seconds * 1000)))
    ms = total_ms % 1000
    total_s = total_ms // 1000
    return (
        f"{total_s // 3600:02d}:{total_s % 3600 // 60:02d}:{total_s % 60:02d},{ms:03d}"
    )


def as_text(segments: list[dict]) -> str:
    return " ".join(s["text"].strip() for s in segments if s.get("text")).strip()


def as_srt(segments: list[dict]) -> str:
    out = []
    for i, seg in enumerate(segments, 1):
        out.append(
            f"{i}\n{stamp(seg['start'])} --> {stamp(seg['end'])}\n{seg['text'].strip()}\n"
        )
    return "\n".join(out)


# ── Giriş noktası ────────────────────────────────────────────────────────────
def main() -> int:
    ap = argparse.ArgumentParser(
        description="WhatsApp sesli notu indirir ve Türkçeye çevirir.",
    )
    ap.add_argument("media_id", help="mesajdaki medya kimliği")
    ap.add_argument("--bridge", default=BRIDGE_BASE, help="köprü taban adresi")
    ap.add_argument("--output", choices=("text", "json", "srt"), default="text")
    args = ap.parse_args()

    wav = ""
    try:
        log(f"[1/3] ses indiriliyor: {args.media_id}")
        data = download_media(args.media_id, args.bridge)
        if not data:
            raise RuntimeError("köprü boş veri döndürdü")
        log(f"      {len(data) / 1024:.0f} KB")

        log("[2/3] wav'a çevriliyor (16 kHz mono)")
        wav = to_wav(data)

        log(f"[3/3] transkript alınıyor ({DEVICE}/{COMPUTE_TYPE})")
        segments = transcribe(wav)

        if args.output == "json":
            print(
                json.dumps(
                    {"media_id": args.media_id, "segments": segments},
                    ensure_ascii=False,
                    indent=2,
                )
            )
        elif args.output == "srt":
            print(as_srt(segments))
        else:
            print(as_text(segments))
        return 0

    except Exception as err:  # noqa: BLE001 — tetikleyiciye tek satır gitsin
        print(f"sesli not çözülemedi: {err}", file=sys.stderr)
        return 1
    finally:
        if wav:
            Path(wav).unlink(missing_ok=True)
            Path(wav).parent.rmdir()


if __name__ == "__main__":
    sys.exit(main())
