#!/usr/bin/env python3
"""cikti.py — Zaman damgalı .txt yazımı, biriktirme.

Her /cmd çalıştırması için yeni bir zaman damgalı dosya oluşturur.
Çıktılar `/home/mfmx/Masaüstü/whatsapp-cmd/` altına yazılır.
"""

from __future__ import annotations

import os
from datetime import datetime
from pathlib import Path
from typing import Optional


# Çıktı klasörü
CIKTI_KLASORU = Path.home() / "Desktop" / "whatsapp-cmd"

# WhatsApp mesaj boyutu sınırı (güvenli marjla)
WHATSAPP_MAKS_UZUNLUK = 3500

# Dosya adı formatı: YYYYMMDD_HHMMSS_mmm.txt
DOSYA_FORMATI = "%Y%m%d_%H%M%S_%f"


def cikti_kirp(metin: str, maks: int = WHATSAPP_MAKS_UZUNLUK) -> str:
    """WhatsApp boyutuna kırp."""
    if len(metin) <= maks:
        return metin
    # Son kısmı al, başta "[kırpıldı]" belirt
    return f"[çıktı kırpıldı, {len(metin)} karakter]\n...\n" + metin[-(maks - 50) :]


def klasor_hazirla() -> Path:
    """Çıktı klasörünü oluştur (yoksa)."""
    CIKTI_KLASORU.mkdir(parents=True, exist_ok=True)
    return CIKTI_KLASORU


def yeni_dosya_adi() -> str:
    """Yeni zaman damgalı dosya adı üret."""
    return datetime.now().strftime(DOSYA_FORMATI)[:-3] + ".txt"  # milisaniye 3 basamak


def yaz(
    icerik: str,
    dosya_adi: Optional[str] = None,
    ekle: bool = False,
) -> Path:
    """İçeriği dosyaya yaz.

    Args:
        icerik: Yazılacak metin
        dosya_adi: Dosya adı (None = yeni zaman damgalı)
        ekle: True = dosya varsa sonuna ekle, False = üzerine yaz

    Returns:
        Yazılan dosyanın yolu
    """
    klasor_hazirla()

    if dosya_adi is None:
        dosya_adi = yeni_dosya_adi()

    dosya_yolu = CIKTI_KLASORU / dosya_adi

    mod = "a" if ekle else "w"
    with open(dosya_yolu, mod, encoding="utf-8") as f:
        f.write(icerik)

    return dosya_yolu


def yaz_ve_don(
    icerik: str,
    dosya_adi: Optional[str] = None,
) -> tuple[Path, str]:
    """İçeriği yaz ve dosya adını da döndür (WhatsApp mesajı için)."""
    yol = yaz(icerik, dosya_adi)
    return yol, yol.name


def listele(limit: int = 20) -> list[Path]:
    """En yeni dosyaları listele (en yeni önce)."""
    klasor_hazirla()
    dosyalar = sorted(
        CIKTI_KLASORU.glob("*.txt"), key=lambda p: p.stat().st_mtime, reverse=True
    )
    return dosyalar[:limit]


def oku(dosya_adi: str) -> Optional[str]:
    """Dosyayı oku."""
    dosya_yolu = CIKTI_KLASORU / dosya_adi
    if not dosya_yolu.exists():
        return None
    with open(dosya_yolu, "r", encoding="utf-8") as f:
        return f.read()


def sil(dosya_adi: str) -> bool:
    """Dosyayı sil."""
    dosya_yolu = CIKTI_KLASORU / dosya_adi
    if dosya_yolu.exists():
        dosya_yolu.unlink()
        return True
    return False


def temizle(gun: int = 7) -> int:
    """Eski dosyaları temizle (gün cinsinden)."""
    klasor_hazirla()
    simdi = datetime.now().timestamp()
    sayac = 0
    for dosya in CIKTI_KLASORU.glob("*.txt"):
        if (simdi - dosya.stat().st_mtime) > (gun * 86400):
            dosya.unlink()
            sayac += 1
    return sayac


if __name__ == "__main__":
    # Test
    klasor_hazirla()
    print(f"Klasör: {CIKTI_KLASORU}")

    yol = yaz("Test içerik 1\nSatır 2\n")
    print(f"Yazıldı: {yol}")

    yol2 = yaz("Test içerik 2\n", ekle=True)
    print(f"Eklendi: {yol2}")

    dosyalar = listele(5)
    print(f"\nSon 5 dosya:")
    for d in dosyalar:
        print(f"  {d.name} ({d.stat().st_size} bytes)")
