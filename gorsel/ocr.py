"""ocr — tesseract ile görselden metin çıkarma.

Neden ayrı katman:
  Tesseract bir saniyede çalışır, ~5 milyon kelime vardır ve vision
  modeline DOKUNMADAN metni çıkarır. Vision modeli metin okumada
  güvenilir değildir — küçük puntolu ekran görüntülerini uydurur.
  Doğrulama (tools/gorsel-test/karsilastir.py) qwen modellerinin
  belge okumada tesseract'ın gerisinde kaldığını gösterdi.

  Bu yüzden akış: OCR ÖNCE çalışır, metni vision modeline "zemin"
  olarak verilir. Vision modeli görür + metni düzeltir.
"""

from __future__ import annotations

import os
import re
import shutil
import subprocess
from pathlib import Path

TESSERACT = shutil.which("tesseract")

# Türkçe + İngilizce: WhatsApp ekran görüntülerinde İngilizce arayüz
# metinleri (Settings, File, Edit) çok yaygın.
VARSAYILAN_DIL = os.getenv("GORSEL_OCR_DIL", "tur+eng")

# Sayfa düzeni denemeleri. Varsayılan 3 (tam otomatik) çok satırlı
# ekran görüntülerinde satırları birbirine katıyor.
PSM_DENEMELERI = os.getenv("GORSEL_OCR_PSM", "6,11,3").split(",")

SURE_S = int(os.getenv("GORSEL_OCR_SURE", "45"))

# Bu karakterlerden fazlası varsa OCR muhtemelen gürültü üretti.
MAKS_GURULTU_ORANI = 0.35
EN_FAZLA_SATIR = 60


class OcrHatasi(RuntimeError):
    """OCR çalışmadı (tesseract yok ya da çöktü)."""


def var_mi() -> bool:
    return TESSERACT is not None


def calistir(
    yol: Path,
    dil: str = VARSAYILAN_DIL,
    psm_dene: list[str] | None = None,
) -> tuple[str, str]:
    """Görselden metin çıkarır.

    Dönüş: (metin, kullanilan_psm). Metin bulunamazsa ("", "").

    Birden fazla sayfa düzeni denenir; hangisi daha çok gerçek metin
    veriyorsa o seçilir. Bu, gürültülü çıktıyı otomatik eler.
    """
    if TESSERACT is None:
        raise OcrHatasi("tesseract bulunamadı (arch: tesseract + tesseract-data-tur)")

    en_iyi = ""
    en_iyi_psm = ""
    for psm in psm_dene or PSM_DENEMELERI:
        metin = _tek_deneme(yol, dil, psm.strip())
        temiz = temizle(metin)
        if not temiz:
            continue
        # Gerçek metin daha uzun kazanır; eşitlikte gürültüsüz olan.
        if len(temiz) > len(en_iyi):
            en_iyi, en_iyi_psm = temiz, psm.strip()

    return en_iyi, en_iyi_psm


def _tek_deneme(yol: Path, dil: str, psm: str) -> str:
    sonuc = subprocess.run(
        [TESSERACT, str(yol), "stdout", "-l", dil, "--psm", psm],
        capture_output=True,
        text=True,
        timeout=SURE_S,
        # Tesseract 3.0x'in yanlış kelime sonuçları üretmesini engelle
        env={**os.environ, "OMP_THREAD_LIMIT": "1"},
    )
    if sonuc.returncode != 0:
        return ""
    return sonuc.stdout


def temizle(metin: str) -> str:
    """OCR çıktısını kullanılabilir hâle getirir.

    • Boş satırları atar
    • Tekrarlanan boşlukları daraltır
    • Aşırı uzun satırları keser (gürültü)
    • Kenar gürültüsü olan kısa satırları eler — AMA yalnız gerçek metin
      varsa. Aksi hâlde ekran görüntüsündeki "OK", "Evet", "Ad", "1"
      gibi kısa ama anlamlı satırlar da silinirdi.
    • Gürültü oranı yüksekse çıktıyı reddeder
    """
    satirlar = []
    for satir in metin.splitlines():
        satir = re.sub(r"[ \t]+", " ", satir).strip()
        if not satir:
            continue
        if len(satir) > 200:
            satir = satir[:200]
        satirlar.append(satir)

    if not satirlar:
        return ""

    # Gürültü oranı: yazı gibi olmayan karakter ağırlığı
    toplam = sum(len(s) for s in satirlar)
    harf = sum(1 for s in satirlar for c in s if c.isalnum() or c.isspace())
    if toplam and (1 - harf / toplam) > MAKS_GURULTU_ORANI:
        return ""

    # Kısa satır eleme. Ölçüt UZUNLUK DEĞİL, İÇERİK: gürültü harf/rakam
    # içermez ("[]", "|", "—", "ı"). "OK", "Ad", "No" gibi 2 harfli
    # kelimeler gerçektir ve kalır.
    #
    # Önceki sürüm len<=2 idi ve "OK"/"Ad" ekran görüntülerinde kayboldu.
    satirlar = [s for s in satirlar if not alfanum_siz(s)]

    return "\n".join(satirlar[:EN_FAZLA_SATIR])


def alfanum_siz(satir: str) -> bool:
    """Satır harf veya rakam içermiyor mu? (gürültü tanımı)"""
    return not any(c.isalnum() for c in satir)


def pdf_metni(pdf: Path) -> str:
    """PDF'ten metin çıkarır — OCR'sız, kesin.

    Dijital PDF'te bu her zaman doğrudur. Taranmış PDF'te boş döner,
    o durumda çağıran taraf sayfayı görsele çevirip OCR yapar.
    """
    pdftotext = shutil.which("pdftotext")
    if pdftotext is None:
        return ""
    sonuc = subprocess.run(
        [pdftotext, "-layout", "-enc", "UTF-8", str(pdf), "-"],
        capture_output=True,
        text=True,
        timeout=SURE_S,
    )
    if sonuc.returncode != 0:
        return ""
    return temizle(sonuc.stdout)


def ozet(metin: str, en_fazla: int = 800) -> str:
    """Metni model istemine sığacak hâle getirir.

    Çok uzun OCR çıktıları istemi şişirir ve modelin görseli atlamasına
    yol açar (belge testinde böyle oldu). Uzun metinler ortadan kesilir,
    kesildiği yere işaret konur.
    """
    metin = metin.strip()
    if len(metin) <= en_fazla:
        return metin
    yari = en_fazla // 2
    return (
        metin[:yari]
        + f"\n… [{len(metin) - en_fazla} karakter atlandı] …\n"
        + metin[-yari:]
    )
