#!/usr/bin/env python3
"""session_manager.py — /session komutları yöneticisi.

- /session list [n]       → Son n session listesi (varsayılan 10)
- /session go <n|id>      → Session tüneline gir
- /session exit           → Ana session'a dön
- /session stop           → Aktif session'u durdur ve tünelden çık
- /session state          → Mevcut session durumu
"""

from __future__ import annotations

import json
import os
import subprocess
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple

from tunel import (
    kullanici_tunel_al,
    kullanici_tunel_guncelle,
    kullanici_tunel_sifirla,
    proje_dizini,
)


def _onbellek_dosya_yolu() -> Path:
    """Son listelenen session'ların geçici önbelleği."""
    state_dir = os.getenv("TETIKLEYICI_STATE_DIR")
    if state_dir:
        dizin = Path(state_dir)
    else:
        dizin = proje_dizini() / ".tetikleyici"
    dizin.mkdir(parents=True, exist_ok=True)
    return dizin / "son_sessionlar.json"


def _opencode_session_listesi_al(limit: int = 10) -> Tuple[List[Dict[str, Any]], Optional[str]]:
    """opencode CLI üzerinden son session listesini çeker."""
    bin_yolu = os.path.expanduser("~/.opencode/bin/opencode")
    if not os.path.exists(bin_yolu):
        bin_yolu = "opencode"

    cmd = [bin_yolu, "session", "list", "--format", "json", "-n", str(limit)]
    try:
        proc = subprocess.run(
            cmd,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            timeout=15,
            cwd=str(proje_dizini()),
        )
        if proc.returncode != 0:
            return [], proc.stderr.strip() or f"Çıkış kodu {proc.returncode}"
        
        veri = json.loads(proc.stdout)
        if isinstance(veri, list):
            # Önbelleğe kaydet
            try:
                with open(_onbellek_dosya_yolu(), "w", encoding="utf-8") as f:
                    json.dump(veri, f, ensure_ascii=False, indent=2)
            except Exception:
                pass
            return veri, None
        return [], "Geçersiz veri formatı"
    except Exception as e:
        return [], str(e)


def _onbellekteki_sessionlar() -> List[Dict[str, Any]]:
    """Önceden çekilmiş session listesini okur."""
    yol = _onbellek_dosya_yolu()
    if not yol.exists():
        return []
    try:
        with open(yol, "r", encoding="utf-8") as f:
            veri = json.load(f)
            if isinstance(veri, list):
                return veri
    except Exception:
        pass
    return []


def _tarih_formatla(zaman_damgasi: Any) -> str:
    """Milisaniye veya saniye zaman damgasını okunur formata çevirir."""
    if not zaman_damgasi:
        return ""
    try:
        val = float(zaman_damgasi)
        if val > 1e11:  # milisaniye
            val = val / 1000.0
        return datetime.fromtimestamp(val).strftime("%Y-%m-%d %H:%M")
    except Exception:
        return str(zaman_damgasi)[:16]


def session_listele(limit: int = 10) -> str:
    """Son session'ları listeler."""
    if limit <= 0:
        limit = 10
    if limit > 200:
        limit = 200

    sessionlar, hata = _opencode_session_listesi_al(limit)
    if hata:
        return f"❌ Session listesi alınamadı: {hata}"

    if not sessionlar:
        return "Açık veya kayıtlı session bulunamadı."

    satirlar = [f"📋 Session Listesi (Son {len(sessionlar)} adet):"]
    for i, s in enumerate(sessionlar, 1):
        baslik = s.get("title") or "İsimsiz"
        sid = s.get("id") or ""
        tarih = _tarih_formatla(s.get("updated") or s.get("created"))
        satir = f"  {i}. {baslik}"
        if tarih:
            satir += f" ({tarih})"
        satirlar.append(satir)
        if sid:
            satirlar.append(f"     ID: {sid}")

    satirlar.append("\n💡 Tünele girmek için: /session go <numara veya ID>")
    return "\n".join(satirlar)


def session_go(hedef: str, gonderen: str) -> str:
    """Belirtilen session'a tünel açar."""
    hedef = hedef.strip()
    if not hedef:
        return "Hata: Session numarası veya ID belirtmelisiniz. Örnek: /session go 1"

    secilen: Optional[Dict[str, Any]] = None

    # Önce önbelleğe bak
    sessionlar = _onbellekteki_sessionlar()

    if hedef.isdigit():
        idx = int(hedef) - 1
        if 0 <= idx < len(sessionlar):
            secilen = sessionlar[idx]
        else:
            # Önbellekte yoksa yeniden çek
            yeni_liste, hata = _opencode_session_listesi_al(max(idx + 10, 50))
            if not hata and 0 <= idx < len(yeni_liste):
                secilen = yeni_liste[idx]
    else:
        # ID veya kısmi ID ile ara
        for s in sessionlar:
            sid = s.get("id", "")
            if sid == hedef or sid.startswith(hedef):
                secilen = s
                break
        if not secilen:
            # Yeniden çekip ara
            yeni_liste, hata = _opencode_session_listesi_al(100)
            if not hata:
                for s in yeni_liste:
                    sid = s.get("id", "")
                    if sid == hedef or sid.startswith(hedef):
                        secilen = s
                        break

    if not secilen:
        return f"❌ Session bulunamadı: {hedef}\nListelemek için: /session list"

    sid = secilen.get("id", "")
    title = secilen.get("title") or "İsimsiz"

    # Tüneli aktifleştir
    kullanici_tunel_guncelle(
        gonderen,
        mod="session",
        session_id=sid,
        session_title=title,
    )

    return (
        f"🎯 Session Tüneli Açıldı!\n\n"
        f"• Başlık: {title}\n"
        f"• ID: {sid}\n\n"
        f"Artık bu sohbete yazacağınız her mesaj doğrudan bu oturuma iletilecek.\n"
        f"🚪 Tünelden çıkmak için: /session exit (veya /exit)"
    )


def session_exit(gonderen: str) -> str:
    """Session tünelini kapatır, ana session'a döner."""
    kullanici_tunel_sifirla(gonderen)
    return "🚪 Session tünelinden çıkıldı. Varsayılan ana oturuma dönüldü."


def session_state(gonderen: str) -> str:
    """Mevcut session durumunu gösterir."""
    tunel = kullanici_tunel_al(gonderen)
    mod = tunel.get("mod", "normal")

    if mod == "session":
        sid = tunel.get("session_id", "bilinmiyor")
        title = tunel.get("session_title", "İsimsiz")
        return (
            f"📊 Session Durumu: TÜNEL AKTİF\n\n"
            f"• Mod: Tünel\n"
            f"• Başlık: {title}\n"
            f"• ID: {sid}\n\n"
            f"Çıkmak için: /session exit veya /exit"
        )
    elif mod == "cmdhub":
        return (
            f"📊 Session Durumu: CMDHUB AKTİF\n\n"
            f"• Mod: CMDHUB Kabuk Tüneli\n"
            f"• Dizin: {tunel.get('cwd', '')}\n\n"
            f"Çıkmak için: /cmdhub exit veya /exit"
        )
    else:
        return (
            f"📊 Session Durumu: NORMAL (Ana Oturum)\n\n"
            f"• Mod: Varsayılan Oturum\n"
            f"• Tünel açmak için: /session list → /session go <numara>"
        )


def session_stop(gonderen: str) -> str:
    """Aktif session tünelini durdurur ve ana session'a döner."""
    tunel = kullanici_tunel_al(gonderen)
    mod = tunel.get("mod", "normal")
    if mod == "session":
        sid = tunel.get("session_id", "")
        kullanici_tunel_sifirla(gonderen)
        return f"🛑 Session durduruldu ({sid}). Tünelden çıkıldı, ana oturuma dönüldü."
    elif mod == "cmdhub":
        kullanici_tunel_sifirla(gonderen)
        return "🛑 CMDHUB tüneli durduruldu. Normal moda dönüldü."
    return "Aktif bir session tüneli yok. Zaten ana oturumdasınız."
