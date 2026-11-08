#!/usr/bin/env python3
"""gorsel/sticker.py — WebP sticker → PNG → OCR → Vision.

Akış:
    WebP sticker indir → PNG çevir (magick) → OCR → eğer metin az/yoksa Vision

Çıktı sözleşmesi: stdout DOLU olmak zorunda. Hata olsa bile Türkçe
açıklama yazar. Boş stdout, tetikleyiciyi "çıktı boş" yoluna düşürür.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Optional

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from gorsel import ocr, gorme, istem

# Araçlar
MAGICK = shutil.which("magick") or shutil.which("convert")
MAKS_KENAR = int(os.getenv("GORSEL_MAKS_KENAR", "1600"))

BOS_MESAJ = "Sticker aldım ama içeriğini okuyamadım."


def log(mesaj: str) -> None:
    print(f"[sticker] {mesaj}", file=sys.stderr, flush=True)


# --- Hazır cevaplar ---


def indirilemedi() -> str:
    return "Stickeri indiremedim, dosya bozuk geldi ya da köprüye ulaşılamadı. Bir daha göndermeyi dene."


def vision_yok() -> str:
    return (
        "Stickeri indirdim ve PNG'ye çevirdim ama bu bilgisayarda görsel okuyan model yok, "
        "bu yüzden anlayamadım.\n\n"
        "Kurulum:  ollama pull "
        + (gorme.VARSAYILAN_MODELLER[0] if gorme.VARSAYILAN_MODELLER else "qwen3-vl:8b")
        + "\nSonra tekrar gönder."
    )


def onisleme_hatasi(hata: Exception) -> str:
    return f"Stickeri indirdim ama işleyemedim ({hata}). Farklı bir dosya dene."


def beklenmeyen(hata: Exception) -> str:
    return f"Sticker işlerken hata oldu ({type(hata).__name__}). Ayrıntı tetikleyici günlüğünde."


# --- WebP → PNG ---


def webp_to_png(webp_yol: Path, cikti_yol: Path) -> bool:
    """WebP sticker'ı PNG'ye çevirir (magick/convert)."""
    if not MAGICK:
        return False

    try:
        sonuc = subprocess.run(
            [
                MAGICK,
                f"{webp_yol}[0]",  # animasyonlu webp'te ilk kare (0 bayt tuzağı)
                "-resize",
                f"{MAKS_KENAR}x{MAKS_KENAR}>",
                str(cikti_yol),
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )
        return (
            sonuc.returncode == 0
            and cikti_yol.exists()
            and cikti_yol.stat().st_size > 0
        )
    except (subprocess.TimeoutExpired, OSError, subprocess.CalledProcessError):
        return False


# --- Sticker işleme ---


def _vision_betimle(yol: Path, ist: str) -> str:
    """Vision modelini seçip tek görseli betimletir."""
    try:
        model = gorme.gorsel_model_sec()
    except gorme.ModelYok:
        raise
    return gorme.sor(model, [yol], ist)


def sticker_isle(dosya: Path) -> str:
    """Yerel .webp sticker dosyasını işler (köprüsüz test)."""
    if dosya.suffix.lower() != ".webp":
        return "Sadece .webp sticker desteklenir."

    # 1. WebP → PNG
    with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as tmp:
        png_yol = Path(tmp.name)

    if not webp_to_png(dosya, png_yol):
        return "Stickeri PNG'ye çeviremedim (ImageMagick yok mu?)."

    try:
        # 2. OCR dene
        ocr_metin, _psm = ocr.calistir(png_yol)
        if ocr_metin and len(ocr_metin.strip()) > 2:
            # Metin varsa: belge modu
            ist = istem.kur("document", "", "serbest", ocr_metni=ocr_metin.strip())
            try:
                # İlk karenin PNG'si modele gider: animasyonlu webp'i statik
                # vision modeline vermek belirsizdir (canlı sticker 23 kare).
                cevap = _vision_betimle(png_yol, ist)
            except gorme.ModelYok:
                return vision_yok()
            return (
                cevap.strip()
                if cevap
                else "Sticker metni okundu ama betimleme üretilmedi."
            )
    finally:
        # Geçici PNG sil
        try:
            os.unlink(png_yol)
        except OSError:
            pass

    # 3. Metin yok/azsa → Vision
    try:
        ist = (
            istem.SISTEM_TONU
            + "\n\nSana bir çıkartma (sticker) gönderdim. Ne anlatıyor, kısaca söyle."
        )
        gorsel = _vision_betimle(png_yol, ist)
        return gorsel.strip() if gorsel else "Sticker görüldü ama betimleme üretilmedi."
    except gorme.ModelYok:
        return vision_yok()
    except Exception as e:
        return f"Vision hatası: {e}"


def sticker_isle_bridge(media_id: str, bridge_base: str) -> str:
    """Köprüden sticker indir → WebP → PNG → OCR → Vision."""
    import urllib.request

    # 1. indir
    url = f"{bridge_base.rstrip('/')}/media/{media_id}"
    try:
        with tempfile.NamedTemporaryFile(suffix=".webp", delete=False) as tmp:
            webp_yol = Path(tmp.name)
        urllib.request.urlretrieve(url, webp_yol)
    except Exception as e:
        return f"Stickeri indiremedim: {e}"

    try:
        # WebP → PNG
        with tempfile.NamedTemporaryFile(suffix=".png", delete=False) as tmp:
            png_yol = Path(tmp.name)

        if not webp_to_png(webp_yol, png_yol):
            return "Stickeri PNG'ye çeviremedim (ImageMagick yok mu?)."

        # OCR dene
        ocr_metin, _psm = ocr.calistir(png_yol)
        if ocr_metin and len(ocr_metin.strip()) > 2:
            ist = istem.kur("document", "", "serbest", ocr_metni=ocr_metin.strip())
            try:
                cevap = _vision_betimle(png_yol, ist)
            except gorme.ModelYok:
                return vision_yok()
            return (
                cevap.strip()
                if cevap
                else "Sticker metni okundu ama betimleme üretilmedi."
            )
        else:
            # Vision'a ilk karenin PNG'si (animasyonlu webp statik modele
            # belirsiz; 0 bayt/webp tuzağı canlıda ölçüldü).
            try:
                ist = (
                    istem.SISTEM_TONU
                    + "\n\nSana bir çıkartma (sticker) gönderdim. Ne anlatıyor, kısaca söyle."
                )
                gorsel = _vision_betimle(png_yol, ist)
                return (
                    gorsel.strip()
                    if gorsel
                    else "Sticker görüldü ama betimleme üretilmedi."
                )
            except gorme.ModelYok:
                return vision_yok()
            except Exception as e:
                return f"Vision hatası: {e}"
    finally:
        # Temizlik
        for y in [webp_yol, png_yol]:
            try:
                if isinstance(y, Path) and y.exists():
                    os.unlink(y)
            except Exception:
                pass


if __name__ == "__main__":
    import sys

    if len(sys.argv) < 2:
        print("Kullanım: sticker.py <media_id|--dosya yol>", file=sys.stderr)
        sys.exit(1)

    arg = sys.argv[1]
    if arg == "--dosya":
        dosya = Path(sys.argv[2])
        print(sticker_isle(dosya))
    else:
        bridge = os.getenv("BRIDGE_BASE", "http://127.0.0.1:31415")
        print(sticker_isle_bridge(arg, bridge))
