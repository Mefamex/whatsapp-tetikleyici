#!/usr/bin/env python3
"""tunel.py — Oturum ve CMDHUB tünel durumu yönetimi.

Kullanıcıların aktif tünel modunu (.tetikleyici/tunel_durumu.json) yönetir:
- "normal": Varsayılan ana oturum
- "session": Belirli bir opencode/ajan session'ına kilitli tünel
- "cmdhub": Doğrudan bash kabuk komutu tüneli
"""

from __future__ import annotations

import json
import os
import tempfile
import time
from pathlib import Path
from typing import Any, Dict, Optional


def proje_dizini() -> Path:
    """Proje kök dizinini bulur."""
    # cmd/ dizininden 2 üst dizin = proje kökü
    return Path(__file__).resolve().parent.parent.parent


def tunel_dosya_yolu() -> Path:
    """Tünel durumu dosya yolunu döndürür."""
    state_dir = os.getenv("TETIKLEYICI_STATE_DIR")
    if state_dir:
        dizin = Path(state_dir)
    else:
        dizin = proje_dizini() / ".tetikleyici"
    dizin.mkdir(parents=True, exist_ok=True)
    return dizin / "tunel_durumu.json"


def tunel_durumu_yukle() -> Dict[str, Dict[str, Any]]:
    """Tüm tünel durumlarını JSON dosyasından yükler."""
    yol = tunel_dosya_yolu()
    if not yol.exists():
        return {}
    try:
        with open(yol, "r", encoding="utf-8") as f:
            veri = json.load(f)
            if isinstance(veri, dict):
                return veri
    except Exception:
        pass
    return {}


def tunel_durumu_kaydet(durumlar: Dict[str, Dict[str, Any]]) -> None:
    """Tünel durumlarını atomik olarak dosyaya kaydeder."""
    yol = tunel_dosya_yolu()
    dizin = yol.parent
    dizin.mkdir(parents=True, exist_ok=True)

    with tempfile.NamedTemporaryFile("w", dir=dizin, delete=False, encoding="utf-8") as tf:
        json.dump(durumlar, tf, ensure_ascii=False, indent=2)
        gecici_yol = tf.name

    os.replace(gecici_yol, yol)


def _temiz_anahtar(gonderen: str) -> str:
    """Numara veya LID'yi normalize eder."""
    return gonderen.strip()


def varsayilan_tunel() -> Dict[str, Any]:
    """Boş/varsayılan tünel kaydı."""
    return {
        "mod": "normal",
        "session_id": "",
        "session_title": "",
        "cwd": str(proje_dizini()),
        "son_komut": "",
        "son_cikti": "",
        "son_cikti_dosyasi": "",
        "guncellendi": int(time.time()),
    }


def kullanici_tunel_al(gonderen: str) -> Dict[str, Any]:
    """Kullanıcının tünel kaydını döndürür."""
    anahtar = _temiz_anahtar(gonderen)
    durumlar = tunel_durumu_yukle()

    # Tam eşleşme ara
    if anahtar in durumlar:
        return durumlar[anahtar]

    # Numara veya @ sonrası eşleşme ara (örn: 905551234567 vs 905551234567@s.whatsapp.net)
    temiz = anahtar.split("@")[0]
    for k, v in durumlar.items():
        if k.split("@")[0] == temiz:
            return v

    return varsayilan_tunel()


def kullanici_tunel_guncelle(gonderen: str, **alanlar: Any) -> Dict[str, Any]:
    """Kullanıcının tünel kaydını günceller ve kaydeder."""
    anahtar = _temiz_anahtar(gonderen)
    durumlar = tunel_durumu_yukle()

    mevcut = kullanici_tunel_al(anahtar)
    mevcut.update(alanlar)
    mevcut["guncellendi"] = int(time.time())

    durumlar[anahtar] = mevcut
    # Eğer numara veya LID varsa iki anahtara da yazarak senkronize et
    if "@" in anahtar:
        durumlar[anahtar.split("@")[0]] = mevcut
    tunel_durumu_kaydet(durumlar)
    return mevcut


def kullanici_tunel_sifirla(gonderen: str) -> Dict[str, Any]:
    """Kullanıcıyı normal moda döndürür."""
    cwd = kullanici_tunel_al(gonderen).get("cwd", str(proje_dizini()))
    return kullanici_tunel_guncelle(
        gonderen,
        mod="normal",
        session_id="",
        session_title="",
        cwd=cwd,
    )


def kullanici_modu(gonderen: str) -> str:
    """Kullanıcının o anki modunu ("normal", "session", "cmdhub") döndürür."""
    return kullanici_tunel_al(gonderen).get("mod", "normal")
