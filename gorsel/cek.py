"""cek — medyayı köprüden indirir, türünü belirler.

Köprüde medya ucu /media/{kimlik} olduğu için buraya KİMLİK verilir,
metin değil. Bu karışıklık canlıda oldu: sesli not kuralı {mesajlar}
ile yazılıydı, script metni kimlik sandı, 404 aldı.
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request
from pathlib import Path

BRIDGE_BASE = os.getenv("BRIDGE_BASE", "http://127.0.0.1:31415")
INDIRME_SURESI = int(os.getenv("GORSEL_INDIRME_SURESI", "120"))

# Uzantı → MIME. Köprü Content-Type vermeyebilir (WhatsApp'ın
# download ucu octet-stream döner), o zaman sihirli baytlara bakılır.
UZANTI_MIME = {
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".png": "image/png",
    ".webp": "image/webp",
    ".gif": "image/gif",
    ".bmp": "image/bmp",
    ".heic": "image/heic",
    ".heif": "image/heif",
    ".tiff": "image/tiff",
    ".pdf": "application/pdf",
    ".txt": "text/plain",
    ".md": "text/markdown",
    ".csv": "text/csv",
    ".json": "application/json",
    ".doc": "application/msword",
    ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    ".xls": "application/vnd.ms-excel",
    ".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    ".ppt": "application/vnd.ms-powerpoint",
    ".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    ".mp4": "video/mp4",
    ".mov": "video/quicktime",
    ".mkv": "video/x-matroska",
    ".webm": "video/webm",
    ".3gp": "video/3gpp",
    ".ogg": "audio/ogg",
    ".oga": "audio/ogg",
    ".opus": "audio/ogg",
    ".m4a": "audio/mp4",
    ".mp3": "audio/mpeg",
    ".wav": "audio/wpeg",
}


class IndirmeHatasi(RuntimeError):
    """Köprüden medya alınamadı."""


def indir(
    medya_kimligi: str, hedef: Path, kopru: str = BRIDGE_BASE
) -> tuple[Path, str]:
    """Medyayı indirir, MIME türünü döndürür.

    Dönüş: (dosya yolu, mime). MIME bilinmiyorsa "" döner; çağıran taraf
    sihirli baytlarla tahmin eder.
    """
    url = f"{kopru.rstrip('/')}/media/{medya_kimligi}"
    istek = urllib.request.Request(url, headers={"User-Agent": "gorsel-pipeline/1"})

    try:
        with urllib.request.urlopen(istek, timeout=INDIRME_SURESI) as cevap:
            veri = cevap.read()
            tur = (cevap.headers.get("Content-Type") or "").split(";")[0].strip()
    except urllib.error.HTTPError as err:
        raise IndirmeHatasi(f"köprü HTTP {err.code} döndü ({err.reason})") from err
    except urllib.error.URLError as err:
        raise IndirmeHatasi(f"köprüye ulaşılamadı: {err.reason}") from err

    if not veri:
        raise IndirmeHatasi("köprü boş veri döndürdü")

    # Uzantı bilinmiyorsa ya da yanlışsa türü yeniden adlandırmaya yetecek
    # kadar veriden tahmin et.
    uzanti = hedef.suffix.lower()
    if uzanti not in UZANTI_MIME:
        tur = tur or tahmin(veri)
        uzanti = MIME_UZANTI.get(tur, ".bin")
        hedef = hedef.with_suffix(uzanti)

    hedef.write_bytes(veri)
    return hedef, (tur if tur and tur != "application/octet-stream" else tahmin(veri))


MIME_UZANTI = {v: k for k, v in UZANTI_MIME.items()}


def tahmin(veri: bytes) -> str:
    """Başlıktan MIME tahmini. Bilinmiyorsa boş döner."""
    imzalar = [
        (b"\xff\xd8\xff", "image/jpeg"),
        (b"\x89PNG\r\n\x1a\n", "image/png"),
        (b"GIF87a", "image/gif"),
        (b"GIF89a", "image/gif"),
        (b"BM", "image/bmp"),
        (b"II*\x00", "image/tiff"),
        (b"MM\x00*", "image/tiff"),
        (b"%PDF", "application/pdf"),
        (b"PK\x03\x04", "application/zip"),  # docx/xlsx/pptx hepsi zip
        (b"\xd0\xcf\x11\xe0", "application/msword"),  # eski doc/xls
        (b"\x1a\x45\xdf\xa3", "video/webm"),  # matroska ve webm
        (b"OggS", "audio/ogg"),
        (b"ID3", "audio/mpeg"),
        (b"\x00\x00\x00\x18ftypmp4", "video/mp4"),
        (b"\x00\x00\x00\x18ftypM4A", "audio/mp4"),
        (b"GIF8", "image/gif"),
    ]
    for imza, mime in imzalar:
        if veri.startswith(imza):
            return mime

    # RIFF kutusu: WEBP mi WAV mi?
    if veri[:4] == b"RIFF" and len(veri) > 12:
        return "image/webp" if veri[8:12] == b"WEBP" else "audio/wav"
    if veri[4:8] == b"ftyp":
        return "video/mp4"

    try:
        veri.decode("utf-8")
        return "text/plain"
    except UnicodeDecodeError:
        return ""


def mesaj_tipi(tur: str) -> str:
    """MIME'ı üst kategoriye indirger: sticker / image / video / audio / document."""
    # WebP önce: WhatsApp sticker'ları image/webp gelir ve STICKER dalına
    # gitmelidir (gorsel_pipeline'ın sticker akışı: webp→png→OCR→vision).
    # Eskiden "image" sayılıyordu; sticker dalı bu yüzden canlıda hiç
    # çalışmıyordu ve animasyonlu webp image yolunda 0 baytlık dosyaya
    # çarpıyordu (canlı ölçüm 2026-10-06, A5F0099C… webp 23 kare).
    if tur == "image/webp":
        return "sticker"
    if tur.startswith("image/"):
        return "image"
    if tur.startswith("video/"):
        return "video"
    if tur.startswith("audio/"):
        return "audio"
    if tur.startswith("text/") or tur in {
        "application/pdf",
        "application/msword",
        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
        "application/vnd.ms-excel",
        "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        "application/vnd.ms-powerpoint",
        "application/vnd.openxmlformats-officedocument.presentationml.presentation",
        "application/json",
        "application/zip",
        "application/vnd.oasis.opendocument.text",
    }:
        return "document"
    return "document" if tur else "bilinmiyor"


def tetikleyici_json(ortam: dict | None = None) -> dict:
    """TRIGGER_JSON'u çözer. Çözülemezse boş sözlük döner.

    Soru çıkarımı için gerekir: mesajın gerçek metni (başlık) burada.
    """
    ham = (ortam or os.environ).get("TRIGGER_JSON", "")
    if not ham.strip():
        return {}
    try:
        veri = json.loads(ham)
    except json.JSONDecodeError:
        return {}
    return veri if isinstance(veri, dict) else {}
