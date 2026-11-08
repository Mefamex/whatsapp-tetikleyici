#!/usr/bin/env python3
"""durum.py — Tetikleyici + köprü anlık durumu (WhatsApp'a uygun düz metin).

`/durum` yazınca çalışır (agent YOK — stdout doğrudan cevap).
Salt okunur: hiçbir şeyi değiştirmez, hiçbir yere yazmaz.
"""

from __future__ import annotations

import json
import os
import time
import urllib.request
from pathlib import Path


BRIDGE_BASE = os.getenv("BRIDGE_BASE", "http://127.0.0.1:31415")
TETIKLEYICI_BIN_ADI = "tetikleyici"


def _proje_koku() -> Path:
    return Path(__file__).resolve().parent.parent.parent


def tetikleyici_pid() -> tuple[int | None, float | None]:
    """Çalışan tetikleyici binary'sinin PID'i + başlama zamanı (epoch).

    `tetikleyici.sh` ile aynı teknik: /proc/*/exe readlink.
    `pgrep -f` kendini eşleştirir, kullanılmaz.
    """
    for pid_dizini in Path("/proc").glob("[0-9]*"):
        try:
            hedef = os.readlink(pid_dizini / "exe")
        except OSError:
            continue
        if Path(hedef).name != TETIKLEYICI_BIN_ADI:
            continue
        # Kendimiz değiliz (python'uz, zaten eşleşmeyiz) — ilk eşleşme alınır.
        try:
            baslama = (pid_dizini / "stat").stat().st_mtime
        except OSError:
            baslama = None
        return int(pid_dizini.name), baslama
    return None, None


def kopru_sagligi(bridge: str = BRIDGE_BASE) -> dict:
    """GET /health. Ulaşılamazsa {'hata': ...}."""
    try:
        with urllib.request.urlopen(
            f"{bridge.rstrip('/')}/health", timeout=10
        ) as yanit:
            return json.loads(yanit.read().decode("utf-8"))
    except Exception as e:  # noqa: BLE001 — kullanıcıya düz metin gider
        return {"hata": str(e)}


def kural_sayisi() -> tuple[int, list[str]]:
    """tetikleyici.yaml'daki AKTİF kural adları (yorum satırları atlanır)."""
    import re

    adlar: list[str] = []
    try:
        with open(_proje_koku() / "tetikleyici.yaml", encoding="utf-8") as f:
            for satir in f:
                s = satir.strip()
                if s.startswith("#"):
                    continue
                m = re.match(r"^-\s*name\s*:\s*(\S+)", s)
                if m:
                    adlar.append(m.group(1))
    except OSError:
        pass
    return len(adlar), adlar


def son_tetikler(n: int = 3) -> list[str]:
    """Logdaki son EŞLEŞTİ satırları (kısa)."""
    log_yolu = _proje_koku() / ".tetikleyici" / "tetikleyici.log"
    try:
        with open(log_yolu, "rb") as f:
            f.seek(0, os.SEEK_END)
            boyut = f.tell()
            # Son ~8KB yeterli (satırlar kısa).
            f.seek(max(0, boyut - 8192))
            ham = f.read().decode("utf-8", errors="replace")
    except OSError:
        return []
    eslesen = [s for s in ham.splitlines() if "EŞLEŞTİ" in s]
    return eslesen[-n:]


def sure_format(saniye: float) -> str:
    """185 -> '3 dk', 7200 -> '2 sa'."""
    if saniye < 60:
        return f"{int(saniye)} sn"
    if saniye < 3600:
        return f"{int(saniye // 60)} dk"
    return f"{saniye / 3600:.1f} sa"


def ana() -> str:
    """Durum metni (stdout sözleşmesi: boş dönmez)."""
    satirlar: list[str] = []

    pid, baslama = tetikleyici_pid()
    if pid is None:
        satirlar.append("Tetikleyici: ÇALIŞMIYOR (süreç bulunamadı)")
    else:
        sure = sure_format(time.time() - baslama) if baslama else "?"
        satirlar.append(f"Tetikleyici: çalışıyor (PID {pid}, {sure})")

    saglik = kopru_sagligi()
    if "hata" in saglik:
        satirlar.append(f"Köprü: ULAŞILAMIYOR ({saglik['hata'][:80]})")
    else:
        durum = "bağlı" if saglik.get("connected") else "eşleşmiş ama soket kapalı"
        satirlar.append(
            f"Köprü: {durum} (uptime {saglik.get('uptime', '?')}, "
            f"tampon {saglik.get('buffered_messages', '?')})"
        )

    sayi, adlar = kural_sayisi()
    satirlar.append(f"Kural: {sayi}" + (f" ({', '.join(adlar)})" if adlar else ""))

    tetikler = son_tetikler()
    if tetikler:
        satirlar.append("Son tetikler:")
        for t in tetikler:
            # "03:59:12 EŞLEŞTİ: Mefamex (...) — 1 kural: mefamex-oturum" kısalt
            parca = t.strip()
            if len(parca) > 100:
                parca = parca[:100] + "…"
            satirlar.append(f"  {parca}")
    else:
        satirlar.append("Son tetik: logda EŞLEŞTİ yok")

    return "\n".join(satirlar)


if __name__ == "__main__":
    print(ana())
