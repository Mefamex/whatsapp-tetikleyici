"""belge — belge türlerinden metin çıkarır.

Kapsam:
    pdf  → pdftotext (kesin, hızlı); boş dönerse sayfa görsele çevrilir
    docx → zip içindeki word/document.xml, etiketler soyularak
    xlsx → zip içindeki sharedStrings.xml + sayfalar
    txt  → doğrudan (kodlama denenir)

Neden kendi kodumuz:
    docx/xlsx "zip" imzalıdır; python'ın zipfile'ı standart kütüphanede
    vardır. Dış araç (antiword, catdoc, libreoffice) kurulu değil ve
    libreoffice ~600 MB getirir. Belgelerin çoğu basit metindir, bu yeter.
"""

from __future__ import annotations

import csv
import io
import json
import re
import subprocess
import zipfile
from pathlib import Path

PDFTOTEXT = None

EN_FAZLA_KARAKTER = 20000


def metin_çikar(yol: Path, mime: str) -> tuple[str, str]:
    """Belgeden metin çıkarır.

    Dönüş: (metin, yöntem). Yöntem: "pdftotext" | "docx" | "xlsx" |
    "düz-metin" | "" (metin yok). Metin yoksa çağıran taraf görsele
    çevirip OCR/vision'a düşer.

    ⚠️ GÖRSEL/VIDEO/SES DOSYALARI ELENİR. Canlıda oltu: bir PNG
    "düz metin" sanılıp binary çöpü 20000 karakterlik metin katmanı
    olarak döndü; OCR bu yüzden hiç çalışmadı ve kullanıcıya görselin
    yerine PNG başlıkları gitti. Ses dosyası da aynı hataya düşerdi.
    """
    global PDFTOTEXT
    if PDFTOTEXT is None:
        import shutil

        PDFTOTEXT = shutil.which("pdftotext")

    mime = (mime or "").lower()

    if mime.startswith(("image/", "video/", "audio/")):
        return "", ""
    if mime and not mime.startswith("text/") and "application/" not in mime:
        return "", ""

    if mime == "application/pdf" or yol.suffix.lower() == ".pdf":
        metin = _pdf(yol)
        if metin:
            return metin[:EN_FAZLA_KARAKTER], "pdftotext"
        return "", ""

    if mime.endswith("wordprocessingml.document") or yol.suffix.lower() in {
        ".docx",
        ".docm",
    }:
        return _office(yol, "word/")[:EN_FAZLA_KARAKTER], "docx"

    if mime.endswith("spreadsheetml.sheet") or yol.suffix.lower() in {".xlsx", ".xlsm"}:
        return _xlsx(yol)[:EN_FAZLA_KARAKTER], "xlsx"

    if mime.endswith("presentationml.presentation") or yol.suffix.lower() in {".pptx"}:
        return _office(yol, "ppt/slides/")[:EN_FAZLA_KARAKTER], "pptx"

    if mime.startswith("text/") or yol.suffix.lower() in {
        ".txt",
        ".md",
        ".csv",
        ".json",
        ".log",
    }:
        return _duz(yol)[:EN_FAZLA_KARAKTER], "düz-metin"

    # .doc (eski ikili biçim) ve bilinmeyen: düz metin dene, olmazsa boş.
    if yol.suffix.lower() == ".doc":
        return "", ""  # antiword yok; çağıran taraf kullanıcıya söyler
    return _duz(yol)[:EN_FAZLA_KARAKTER], "düz-metin"


def _pdf(yol: Path) -> str:
    if not PDFTOTEXT:
        return ""
    try:
        sonuc = subprocess.run(
            [PDFTOTEXT, "-layout", "-enc", "UTF-8", str(yol), "-"],
            capture_output=True,
            text=True,
            timeout=60,
        )
    except subprocess.TimeoutExpired:
        return ""
    if sonuc.returncode != 0:
        return ""
    return _duzla(sonuc.stdout)


def _office(yol: Path, onek: str) -> str:
    """docx/pptx: zip içindeki XML dosyalarından metin.

    Word `<w:t>`, PowerPoint `<a:t>` etiketlerinde metin taşır.
    Paragraf sonu için </w:p> yeni satıra çevrilir.
    """
    parcalar: list[str] = []
    try:
        with zipfile.ZipFile(yol) as z:
            adlar = sorted(
                n for n in z.namelist() if n.startswith(onek) and n.endswith(".xml")
            )
            for ad in adlar:
                ham = z.read(ad).decode("utf-8", "replace")
                metin = re.sub(r"<w:p[ >]|</w:p>|<a:p>|</a:p>", "\n", ham)
                metin = re.sub(r"<[^>]+>", "", metin)
                metin = _coz(metin)
                if metin.strip():
                    parcalar.append(metin.strip())
    except (zipfile.BadZipFile, OSError):
        return ""
    return "\n\n".join(parcalar)


def _xlsx(yol: Path) -> str:
    """xlsx: sharedStrings + satırlar.

    Hücreler `<c r="A1" t="s"><v>3</v></c>` biçiminde; `t="s"` ise
    paylaşılan metin tablosundan (`sharedStrings.xml`) sıra numarası.
    """
    try:
        with zipfile.ZipFile(yol) as z:
            paylasilan = _paylasilan_metinler(z)
            sayfalar = sorted(n for n in z.namelist() if n.startswith("xl/worksheets/"))
            satirlar: list[str] = []
            for sayfa in sayfalar:
                ham = z.read(sayfa).decode("utf-8", "replace")
                for satir in re.findall(r"<row[^>]*>(.*?)</row>", ham, flags=re.DOTALL):
                    hucreler = []
                    for nitelik, icerik in re.findall(
                        r"<c\b([^>]*)>(.*?)</c>", satir, flags=re.DOTALL
                    ):
                        tur = re.search(r't="([^"]+)"', nitelik)
                        deger_m = re.search(r"<v>(.*?)</v>", icerik, flags=re.DOTALL)
                        if not deger_m:
                            continue
                        deger = deger_m.group(1)
                        if tur and tur.group(1) == "s":
                            try:
                                deger = paylasilan[int(deger)]
                            except (ValueError, IndexError):
                                continue
                        else:
                            deger = _coz(deger)
                        if deger.strip():
                            hucreler.append(deger.strip())
                    if hucreler:
                        satirlar.append(" | ".join(hucreler))
    except (zipfile.BadZipFile, OSError):
        return ""
    return "\n".join(satirlar)


def _paylasilan_metinler(z: zipfile.ZipFile) -> list[str]:
    try:
        ham = z.read("xl/sharedStrings.xml").decode("utf-8", "replace")
    except KeyError:
        return []
    return [_coz(m) for m in re.findall(r"<si>(.*?)</si>", ham, flags=re.DOTALL)]


def _duz(yol: Path) -> str:
    """Düz metin dosyası. Kodlama sırası: utf-8 → cp1254 → latin-1.

    ⚠️ SONUÇTA İKİ KONTROL VAR, ikisi de şart:
      latin-1 HİÇBİR baytı reddetmez — yani kodlama denemeleri sonunda
      dosya "metin" sayılır. Olmadan PNG/ELF/zip çöpü belge sanılır.
      Bu yüzden (a) NUL baytı ve (b) denetim karakteri oranı ölçülür.
    """
    ham = yol.read_bytes()
    if not ham:
        return ""
    if _binary_mi(ham):
        return ""

    for kodlama in ("utf-8", "cp1254", "latin-1"):
        try:
            return _duzla(ham.decode(kodlama))
        except UnicodeDecodeError:
            continue
    return ""


# NUL geçen dosya kesin metin değildir (metin dosyalarında NUL olmaz).
# UTF-16 metin NUL içerir, o yüzden NUL oranı yüksekse utf-16 denenir.
def _binary_mi(ham: bytes) -> bool:
    """Bayt dizisi metin mi, yoksa binary mı?"""
    nul_orani = ham.count(b"\x00") / len(ham)
    if nul_orani > 0.3:
        return not _utf16_mi(ham)

    # NUL yoksa kontrol karakterlerine bak: metinde \n \r \t dışında
    # 0x00-0x08, 0x0B, 0x0C, 0x0E-0x1F neredeyse hiç bulunmaz.
    denetim = sum(1 for b in ham if b < 0x20 and b not in (0x09, 0x0A, 0x0D, 0x0C))
    return (denetim / len(ham)) > 0.05


def _utf16_mi(ham: bytes) -> bool:
    """UTF-16 olduğu iddiası doğrulanabiliyor mu?

    UTF-16LE'de ASCII aralığı 0x00 0x4X çiftleriyle görünür: NUL'lar
    tek tek (tek sayı) gelir.
    """
    cift = ham[:2000]
    if len(cift) < 20:
        return False
    tek = sum(1 for i in range(0, len(cift) - 1, 2) if cift[i + 1] == 0)
    return (tek / (len(cift) // 2)) > 0.3


def _coz(metin: str) -> str:
    return _coz_entite(metin)


def _coz_entite(metin: str) -> str:
    """XML/HTML kaçış dizilerini çözer."""
    for karsi, gercek in (
        ("&amp;", "&"),
        ("&lt;", "<"),
        ("&gt;", ">"),
        ("&quot;", '"'),
        ("&apos;", "'"),
        ("&#39;", "'"),
        ("&nbsp;", " "),
    ):
        metin = metin.replace(karsi, gercek)
    metin = re.sub(r"&#(\d+);", lambda m: chr(int(m.group(1))), metin)
    return metin


def _duzla(metin: str) -> str:
    """Satırları temizler, boşları atar."""
    satirlar = []
    for satir in metin.splitlines():
        satir = re.sub(r"[ \t ]+", " ", satir).strip()
        if satir:
            satirlar.append(satir)
    return "\n".join(satirlar)


def csv_olarak(metin: str) -> list[list[str]]:
    """Ayraçlı metni tabloya çevirir — modele "tablo şöyle" demek için."""
    try:
        return list(csv.reader(io.StringIO(metin), delimiter="|"))
    except csv.Error:
        return []


def ozet_istatistik(metin: str) -> dict:
    """Belge hakkında tek satırlık bilgi — cevaba değil, günlüğe."""
    satirlar = [s for s in metin.splitlines() if s.strip()]
    kelimeler = len(metin.split())
    return {
        "karakter": len(metin),
        "satir": len(satirlar),
        "kelime": kelimeler,
        "ortalama_satir_uzunlugu": round(len(metin) / max(1, len(satirlar)), 1),
    }


def json_kisa(veri: dict) -> str:
    """Tanıklama için JSON çıktısı."""
    return json.dumps(veri, ensure_ascii=False, indent=2)
