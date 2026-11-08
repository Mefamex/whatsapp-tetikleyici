#!/usr/bin/env python3
"""cmdhub.py — /cmdhub komut merkezi ve doğrudan kabuk tüneli.

- /cmdhub              → CMDHUB tünel modunu aç
- /cmdhub exit         → CMDHUB modundan çık (veya /exit)
- /cmdhub cd <dizin>   → CMDHUB çalışma dizinini değiştir
- /cmdhub out          → Son çalıştırılan komutun tam çıktısını getir
- /cmdhub state        → CMDHUB durumunu göster
"""

from __future__ import annotations

import os
import subprocess
import time
from pathlib import Path
from typing import Any, Dict, Optional

from cikti import cikti_kirp, yaz
from tunel import (
    kullanici_tunel_al,
    kullanici_tunel_guncelle,
    kullanici_tunel_sifirla,
    proje_dizini,
)


def cmdhub_yonet(
    alt_komut: str,
    arg: str,
    gonderen: str,
    debug_cikti: bool = False,
) -> str:
    """CMDHUB kontrol komutlarını yönetir."""
    alt = alt_komut.lower().strip()
    tunel = kullanici_tunel_al(gonderen)
    cwd = tunel.get("cwd") or str(proje_dizini())

    if alt in ("on", "enter", "start", ""):
        kullanici_tunel_guncelle(gonderen, mod="cmdhub", cwd=cwd)
        return (
            "🖥️ CMDHUB Tüneli Açıldı!\n\n"
            f"• Çalışma dizini: {cwd}\n"
            "• Mod: Doğrudan Kabuk (bash)\n\n"
            "Artık bu sohbete yazacağınız her mesaj doğrudan bash kabuk komutu olarak "
            "çalıştırılacak ve çıktısı sohbete iletilecektir.\n\n"
            "Komutlar:\n"
            "• /cmdhub exit veya /exit → Normal AI moduna dön\n"
            "• /cmdhub out            → Son komutun tam çıktısını al\n"
            "• /cmdhub cd <dizin>     → Çalışma dizinini değiştir\n"
            "• /cmdhub state          → Durumu göster"
        )

    if alt in ("exit", "off", "stop"):
        kullanici_tunel_sifirla(gonderen)
        return "🚪 CMDHUB tüneli kapatıldı. Normal oturum moduna dönüldü."

    if alt in ("state", "status"):
        mod = tunel.get("mod", "normal")
        durum_metni = "AKTİF" if mod == "cmdhub" else "PASİF"
        son_komut = tunel.get("son_komut") or "Henüz komut çalıştırılmadı."
        return (
            f"📊 CMDHUB Durumu: {durum_metni}\n\n"
            f"• Çalışma dizini: {cwd}\n"
            f"• Son komut: {son_komut}\n"
            f"• Son çıktı dosyası: {tunel.get('son_cikti_dosyasi') or 'Yok'}\n\n"
            "Çıkmak için: /cmdhub exit veya /exit"
        )

    if alt == "cd":
        hedef = arg.strip()
        if not hedef:
            return f"Mevcut çalışma dizini: {cwd}\nKullanım: /cmdhub cd <dizin>"
        hedef_yol = Path(os.path.expanduser(hedef)).resolve()
        if not hedef_yol.is_dir():
            return f"❌ Hata: Dizin bulunamadı: {hedef_yol}"
        kullanici_tunel_guncelle(gonderen, cwd=str(hedef_yol))
        return f"📁 Çalışma dizini değiştirildi:\n{hedef_yol}"

    if alt in ("out", "output"):
        son_cikti = tunel.get("son_cikti", "").strip()
        son_dosya = tunel.get("son_cikti_dosyasi", "")
        if not son_cikti:
            return "Henüz çalıştırılmış bir komut çıktısı yok."
        ek = f"\n\nDosya: {son_dosya}" if son_dosya else ""
        return f"📜 Son Komut Çıktısı (Tam):\n\n{son_cikti}{ek}"

    # Bilinmeyen alt komut verilmişse doğrudan kabuk komutu olarak çalıştır
    tam_komut = f"{alt} {arg}".strip() if arg else alt
    return cmdhub_calistir(tam_komut, gonderen, debug_cikti)


def cmdhub_calistir(komut: str, gonderen: str, debug_cikti: bool = False) -> str:
    """Komutu bash kabuğunda çalıştırır ve çıktısını kaydeder."""
    komut = komut.strip()
    if not komut:
        return "Boş komut girildi."

    tunel = kullanici_tunel_al(gonderen)
    cwd = tunel.get("cwd") or str(proje_dizini())
    if not Path(cwd).is_dir():
        cwd = str(proje_dizini())

    baslangic = time.time()
    try:
        proc = subprocess.run(
            ["bash", "-c", komut],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=120,
            cwd=cwd,
        )
        gecen = time.time() - baslangic
        cikis_kodu = proc.returncode
        stdout = proc.stdout
        stderr = proc.stderr
        zaman_asimi = False
    except subprocess.TimeoutExpired as te:
        gecen = time.time() - baslangic
        cikis_kodu = 124
        stdout = te.stdout or ""
        stderr = te.stderr or ""
        zaman_asimi = True
    except Exception as e:
        gecen = time.time() - baslangic
        cikis_kodu = 1
        stdout = ""
        stderr = str(e)
        zaman_asimi = False

    # Çıktı metnini derle
    parcalar = [
        f"Komut: {komut}",
        f"Dizin: {cwd}",
        f"Çıkış kodu: {cikis_kodu} ({gecen:.2f}s)",
    ]
    if zaman_asimi:
        parcalar.append("⚠ Zaman aşımı (120s)")

    if stdout.strip():
        parcalar.append(f"--- STDOUT ---\n{stdout.strip()}")
    if stderr.strip():
        parcalar.append(f"--- STDERR ---\n{stderr.strip()}")

    if not stdout.strip() and not stderr.strip():
        parcalar.append("(Çıktı üretilmedi)")

    tam_metin = "\n".join(parcalar)

    # Dosyaya kaydet
    try:
        dosya_yolu = yaz(tam_metin + "\n")
        dosya_adi = dosya_yolu.name
    except Exception:
        dosya_adi = "kaydedilemedi"

    parcalar.append(f"\nDosya: {dosya_adi}")
    tam_cikti = "\n".join(parcalar)

    # Tünel kaydını güncelle
    kullanici_tunel_guncelle(
        gonderen,
        son_komut=komut,
        son_cikti=tam_cikti,
        son_cikti_dosyasi=dosya_adi,
    )

    if debug_cikti:
        return tam_cikti

    # WhatsApp mesaj uzunluk sınırına göre kırp
    kirpilmis = cikti_kirp(tam_cikti)
    if len(tam_cikti) > len(kirpilmis):
        kirpilmis += f"\n\n(Tam çıktı için: /cmdhub out · {dosya_adi})"
    return kirpilmis
