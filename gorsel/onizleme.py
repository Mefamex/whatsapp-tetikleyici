"""onizleme — görseli modele vermeye uygun hâle getirir.

Neden gerekli:
  • WhatsApp telefon kamerası 12 MP gönderir. qwen-vl 2000+ piksel
    kenarında hem yavaşlar hem de taşar; 8 GB VRAM'de "image too large"
    hatası verir.
  • HEIC (iPhone) ImageMagick'in derleme sürümünde desteklenmeyebilir.
    Desteklenmiyorsa hata mesajı kullanıcıya yansır.
  • Şeffaf PNG, arka planı siyahtır; vision modeli "karanlık fotoğraf"
    sanır. Beyaz zemin ön arka planı doğru okunur hâle getirir.
  • EXIF yönelimi (rotate) uygulanmazsa model görseli yatay sanır.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

# Modele verilecek en büyük kenar. 1600 px, hem küçük ekranlı metinleri
# hem de genel sahneyi okumaya yeter; süreyi 4-5 kat düşürür.
MAKS_KENAR = int(os.getenv("GORSEL_MAKS_KENAR", "1600"))

# 8 GB VRAM'de qwen3-vl:8b + 1600 px görsel sığmıyor. 1400 güvenli taraf.
MAKS_PIXEL = int(os.getenv("GORSEL_MAKS_PIXEL", str(1400 * 1400)))

JPEG_KALITE = int(os.getenv("GORSEL_JPEG_KALITE", "88"))

# ⛔ ESKİ ADI `GORSEL_KARE_SURESI` İDİ, O DA KALDIRILDI.
# Aynı değişken adı İKİ BİRBİRİYLE İLGİSİZ şeyleri kontrol ediyordu:
# burada ffmpeg ALT SÜREÇ zaman aşımı (60 sn), video.py'de ise vision
# modeline verilen SORU süresiydi (300 sn). Aynı ada iki farklı varsayılan
# yazılmıştı — hangisinin geçerli olduğu dosyaya bakmadan anlaşılmıyordu.
# Daha kötüsü: kullanıcı vision'a süre vermek için bu değişkeni yükseltirse
# ffmpeg'e de aynı süre veriyor, tersi de doğru: ffmpeg'i hızlandırmak için
# 30 sn yazarsa vision çağrısı da 30 sn'ye düşer — ölçülen tek kare süresi
# 32 sn, yani model her seferinde zaman aşımına uğrar.
# Artık ayrı: ffmpeg alt süreci = GORSEL_FFMPEG_SURESI (60),
# vision sorusu = GORSEL_KARE_CAGRI_SURESI (120).
FFMPEG_SURESI = int(os.getenv("GORSEL_FFMPEG_SURESI", "60"))

IMAGEMAGICK = shutil.which("magick") or shutil.which("convert")
FFMPEG = shutil.which("ffmpeg")


class OnIslemeHatasi(RuntimeError):
    """Görsel hazırlanamadı."""


def hazirla(kaynak: Path, hedef: Path) -> dict:
    """Görseli hedefe dönüştürür: JPEG, sınırlı boyut, yönelim düzeltilmiş.

    Dönüş: {"genislik", "yukseklik", "bayt", "ozet", "sikistirildi"}
    """
    if IMAGEMAGICK is None:
        raise OnIslemeHatasi("ImageMagick bulunamadı (magick ya da convert)")

    # identify ile ölç: tek komutta hem bilgi hem dönüşüm.
    # `[0]` ilk kareyi seçer: animasyonlu webp/gif'i magick JPG'ye yazarken
    # rc=0 ile 0 BAYTLIK dosya üretiyor (canlı sticker A5F0099C…, 23 kare).
    # Statik görsellerde `[0]` zararsızdır (tek kare).
    komut = [
        IMAGEMAGICK,
        f"{kaynak}[0]",
        "-auto-orient",  # EXIF yönünü uygula
        "-alpha",
        "remove",
        "-background",
        "white",
        "-alpha",
        "off",  # şeffaflık → beyaz
        "-colorspace",
        "sRGB",
        "-resize",
        f"{MAKS_KENAR}x{MAKS_KENAR}>",  # sadece küçült
        "-quality",
        str(JPEG_KALITE),
        "-strip",  # EXIF/gizli veri sızmasın
        "-interlace",
        "Plane",
        str(hedef),
    ]
    sonuc = subprocess.run(komut, capture_output=True, text=True, timeout=FFMPEG_SURESI)
    # 0 bayt da HATA sayılır: rc=0 olsa bile boş dosya modele giderse
    # sessizce "anlayamadım" üretir (ölçülen animasyonlu webp tuzağı).
    if sonuc.returncode != 0 or not hedef.exists() or hedef.stat().st_size == 0:
        detay = (sonuc.stderr or "").strip().splitlines()
        son = detay[-1] if detay else "üretilen dosya boş (0 bayt)"
        raise OnIslemeHatasi(f"görsel dönüştürülemedi: {son}")

    return {"genislik": 0, "yukseklik": 0, "bayt": hedef.stat().st_size}


def olc(yol: Path) -> tuple[int, int]:
    """(genişlik, yükseklik) döndürür. Ölçemezse (0, 0)."""
    if IMAGEMAGICK is None:
        return (0, 0)
    sonuc = subprocess.run(
        [IMAGEMAGICK, "identify", "-format", "%w %h", f"{yol}[0]"],
        capture_output=True,
        text=True,
        timeout=20,
    )
    if sonuc.returncode != 0:
        return (0, 0)
    parcalar = sonuc.stdout.split()
    if len(parcalar) < 2:
        return (0, 0)
    return (int(parcalar[0]), int(parcalar[1]))


def kare_cikar(video: Path, hedef_dizin: Path, adet: int = 3) -> list[Path]:
    """Videodan eşit aralıklı kare çıkarır.

    Sesli video: kullanıcının konuştuğu şeyi görmek istiyor, videoyu
    tamamlamak değil. Kareler ortaya eşit dağıtılır; hepsi siyahsa
    (kapalı sahne) ilk kare atılır.
    """
    if FFMPEG is None:
        raise OnIslemeHatasi("ffmpeg bulunamadı")

    # Süreyi ffprobe ile ölç; bilinemezse 3 kare için tahmin et.
    sure = video_suresi(video)
    if sure <= 0:
        sure = 6.0

    kareler: list[Path] = []
    for i in range(adet):
        # İlk karenin %20'si sonrasından başla: siyah açılış karesini atla.
        oran = 0.20 + (0.75 * i / max(1, adet - 1)) if adet > 1 else 0.4
        saniye = sure * oran
        yol = hedef_dizin / f"kare{i}.jpg"
        sonuc = subprocess.run(
            [
                FFMPEG,
                "-y",
                "-loglevel",
                "error",
                "-ss",
                f"{saniye:.2f}",
                "-i",
                str(video),
                "-frames:v",
                "1",
                "-vf",
                f"scale='min({MAKS_KENAR},iw)':-2",
                "-q:v",
                "3",
                str(yol),
            ],
            capture_output=True,
            text=True,
            timeout=FFMPEG_SURESI,
        )
        if sonuc.returncode == 0 and yol.exists() and yol.stat().st_size > 0:
            kareler.append(yol)

    return kareler


def video_suresi(video: Path) -> float:
    """Videonun süresi (saniye). ffprobe yoksa veya okunamazsa 0."""
    ffprobe = shutil.which("ffprobe")
    if ffprobe is None:
        return 0.0
    sonuc = subprocess.run(
        [
            ffprobe,
            "-v",
            "error",
            "-show_entries",
            "format=duration",
            "-of",
            "default=noprint_wrappers=1:nokey=1",
            str(video),
        ],
        capture_output=True,
        text=True,
        timeout=30,
    )
    try:
        return max(0.0, float(sonuc.stdout.strip()))
    except ValueError:
        return 0.0


def pdf_ilk_sayfa(pdf: Path, hedef: Path, dpi: int = 150) -> Path | None:
    """PDF'in ilk sayfasını görsele çevirir (görme modeli PDF de alır ama
    metin çıkarmak daha güvenilir; ikisi birlikte kullanılır)."""
    pdftoppm = shutil.which("pdftoppm")
    if pdftoppm is None:
        return None
    sonuc = subprocess.run(
        [
            pdftoppm,
            "-png",
            "-r",
            str(dpi),
            "-f",
            "1",
            "-l",
            "1",
            str(pdf),
            str(hedef.with_suffix("")),
        ],
        capture_output=True,
        text=True,
        timeout=FFMPEG_SURESI,
    )
    uretilen = hedef.with_suffix(".png")
    if sonuc.returncode == 0 and uretilen.exists():
        return uretilen
    # pdftoppm -1.png yerine -01.png de yazabilir
    adaylar = sorted(hedef.parent.glob(hedef.stem + "*.png"))
    return adaylar[0] if adaylar else None
