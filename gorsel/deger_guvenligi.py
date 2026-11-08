#!/usr/bin/env python3
"""deger_guvenligi.py — cevap, görseldeki değerleri gerçekten taşıyor mu?

ÖLÇÜLMÜŞ SORUN
--------------
tools/prompt_varyant.py, d-kucuk.png'de 9 koşu yaptı (3 istem × 3 tekrar).
9/9'unda model şunu üretti:

    «Görseldeki metin, kullanıcı adı ve soyadı, MARUZ ATILMA KODU, SERİ NO,
      ODENEN tutar, VADE TARIHI, IBAN, AÇIKLAMA... bilgilerini içeriyor.»

tesseract bu görselde 5/5 değeri doğru okudu (KX-7741, 88213-D, 4.960,50,
12.11.2026, TR33 …) ve bu metin modele de verildi. Model yine de
alan ADLARINI sayıp DEĞERLERİ atıyor.

Neden: serbest anlatım istendiğinde model 10 alandan bir SEÇİM yapmak
zorunda ve seçimi alan adlarını saymakla geçiriyor. Kullanıcı somut soru
sorduğunda ("toplam ne") seçim yok — tek bir değer vermesi gerekiyor ve
DOĞRU veriyor (ölçüldü: «Toplam 4.960,50 TL.»).

YANİ SORUN MODELİN DEĞİL, İSTEK TÜRÜNÜN. Ama kökten çözmek yerine
belirleyici bir ağ kuruyoruz: cevap değerleri taşımıyorsa OCR özetine düş.

NEDEN AYRI MODÜL
----------------
Bu bir "güzel yazım" işi değil, ölçüme dayalı bir kural. Kuralların
kendisi burada toplanır ki, kural değişirse tek yer değişsin.
"""

from __future__ import annotations

import re

# ── OCR metninden "değer" çıkarma ────────────────────────────────────────────
#
# Değer = kullanıcının işine yarayan somut bilgi: tutar, tarih, kod, numara.
# ALAN ADLARI değer değildir ("ODENEN tutar" bir değer değildir, "4.960,50" değerdir).

# Binlik ayracı olan veyaondalık içeren sayı: 4.960,50 · 12.11.2026 · 23.050
_SAYI = re.compile(r"\d{1,3}(?:[.,]\d{3})+(?:,\d{2})?|\d{1,3}[./-]\d{1,3}[./-]\d{2,4}")

# Neden etiket/tutar AYIRIMI YAPMIYORUZ
# -------------------------------------
# Önce "alan adını değerden ayıklayan" bir filtre (_ALAN_ADI) vardı ve
# TÜM değerleri eliyordu. Sebebi şu: form düzeninde etiket değerden hemen
# önce gelir ("ODENEN: 4.960,50 TL"), yani "öncesinde etiket var mı" diye
# bakmak istediğimiz her şeyi dışlıyordu — d-kucuk.png'deki 9 başarısızlığın
# 0'ını yakalıyordu (ölçüldü, test kırmızıydı).
#
# Doğrusu basit: OCR metnindeki HER sayı ve kod bir değerdir. Etiket zaten
# sözcüktür, sayı değildir; ayırmaya gerek yok.
_KOD = re.compile(r"\b[A-Za-z0-9]{1,6}[-\u2013][A-Za-z0-9]{2,8}\b")

# IBAN gibi uzun rakam dizisi
_UZUN_RAKAM = re.compile(r"\b\d{9,}\b")


def degerleri(metin: str) -> list[str]:
    """OCR metninden somut değerleri çıkarır (tutarlar, tarihler, kodlar)."""
    if not metin:
        return []

    bulunan: list[str] = []
    for desen in (_SAYI, _KOD, _UZUN_RAKAM):
        for eslesme in desen.finditer(metin):
            parca = eslesme.group(0).strip(" .,;:")
            if parca and parca not in bulunan:
                bulunan.append(parca)
    return bulunan


def _rakamlar(metin: str) -> str:
    """Metindeki tüm rakamları tek dizeye indirger: '4.960,50' → '496050'."""
    return re.sub(r"\D", "", metin or "")


def _tasiyor_mu(cevap: str, deger: str) -> bool:
    """Cevap, verilen değeri (toleranslı) içeriyor mu?

    Tolerans neden: OCR '4.960,50' der, model '4.960 TL' diyebilir. Bu bir
    HATA olabilir ama değer AKTARILMIŞTIR — bilgi kaybı değildir. Bu kuralın
    işi "içerik hiç geçti mi" diye bakmaktır, "kuruşu düştü mü" değil.

    O ayrım ölçüldü: kaba eşleşme reddedildiğinde (tam dizi araması) modelin
    iyi cevabı ham OCR dökümüyle değiştirilirdi — kullanıcı daha kötü
    dururdu. Test: test_kabaca_eslesme_kabul.

    Ölçüt: cevaptaki rakam dizisinde değerin en az 3 haneli bir kesiti
    geçiyorsa "aktarılmış" sayılır.
    """
    if not deger:
        return False
    if deger.lower() in cevap.lower():
        return True

    hedef = _rakamlar(deger)
    if len(hedef) < 3:
        # Tek haneli eşleşme anlamsız: "50 adet" cevabı "4.960,50"yi
        # taşımış sayılmaz.
        return False

    cevap_rakamlar = _rakamlar(cevap)
    if not cevap_rakamlar:
        return False

    return any(hedef[i : i + 3] in cevap_rakamlar for i in range(len(hedef) - 2))


def kayip_mi(cevap: str, ocr_metni: str) -> tuple[bool, str]:
    """Cevap OCR'daki değerleri taşımıyor mu?

    Dönüş: (kayıp_mı, gerekçe). gerekçe loga yazılır — hangi kural
    tetiklendiği görünür olmalı, yoksa "neden cevap değişti" sorusu
    cevapsız kalır.

    ⚠️ BU KURAL TERS YÖNDE HATA YAPMAZ. Şüpheliyse geçmez; çünkü
    yanlış tetikleme kullanıcıya modelin cevabı yerine ham OCR
    dökümünü gönderir. Kullanıcının okuyacağı cevap bozulur.
    """
    if not cevap or not ocr_metni:
        return False, ""

    degerler = degerleri(ocr_metni)
    if len(degerler) < 2:
        # OCR'da en az iki somut değer yok — bu kural burada geçerli değil.
        return False, ""

    # KURAL 1 — cevapta hiç rakam yok.
    # 9/9 ölçülen başarısızlık tam olarak bu: alan adlarını sayıp
    # bırakmak. OCR'da rakam varken cevapta hiç yoksa bilgi taşınmamıştır.
    if not _rakamlar(cevap) and any(_rakamlar(d) for d in degerler):
        return True, (
            "cevapta hiç rakam yok, OCR'da "
            f"{len(degerler)} değer var → içerik taşınmamış"
        )

    # KURAL 2 — cevapta rakam var ama OCR'ın HİÇBİR değeri yok.
    # Kısmi aktarım kabul edilir; ama tek bir değer bile geçmiyorsa
    # cevap başka bir şeyden (alan adlarından) bahsediyor demektir.
    gecen = [d for d in degerler if _tasiyor_mu(cevap, d)]
    if not gecen:
        return True, (
            "OCR'daki değerlerin hiçbiri cevapta yok "
            f"({len(degerler)} değer: {', '.join(degerler[:4])})"
        )

    return False, ""
