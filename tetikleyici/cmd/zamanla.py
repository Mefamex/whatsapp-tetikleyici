#!/usr/bin/env python3
"""zamanla.py — /zamanla komutu: "şu zaman sonra şu mesajı gönder".

Zamanlanmış mesaj işlerini iş dizinine yazar; gönderimi tetikleyicinin
kendisi yapar (zamanlanmis.go tarayıcısı). Bu modül yalnız:

    list               → bekleyen işleri listeler
    iptal <id>         → bir işi siler
    <süre> <mesaj>     → iş ekler (10dk, 2sa, 1gun, 45sn veya 22:30)

Her iş ayrı dosyadır (.tetikleyici/zamanlanmis/<id>.json): komut ekler,
tarayıcı gönderip siler — tek ortak dosyada iki yazar yarışmaz.
"""

from __future__ import annotations

import json
import os
import re
import time
from datetime import datetime
from pathlib import Path
from zoneinfo import ZoneInfo

# En az 1 dakika, en çok 7 gün sonra.
MIN_SANIYE = 60
MAX_SANIYE = 7 * 24 * 3600

KAPALI_MESAJI = (
    "❌ zamanlanmış mesaj kapalı.\n"
    "Açmak için: /zamanla aç\n"
    "(kapatmak için: /zamanla kapat)"
)

SURE_PATTERN = re.compile(r"^(\d+)\s*(sn|dk|sa|saat|gun|gün)$", re.IGNORECASE)
SAAT_PATTERN = re.compile(r"^(\d{1,2}):(\d{2})$")

_BIRIM_SANIYE = {
    "sn": 1,
    "dk": 60,
    "sa": 3600,
    "saat": 3600,
    "gun": 86400,
    "gün": 86400,
}

_ISTANBUL = ZoneInfo("Europe/Istanbul")


def _dizin() -> Path:
    """İş dizini: env'den, yoksa proje .tetikleyici/zamanlanmis/."""
    if d := os.environ.get("ZAMANLANMIS_DIZIN", "").strip():
        return Path(d)
    return Path(__file__).resolve().parents[2] / ".tetikleyici" / "zamanlanmis"


def _acik_mi() -> bool:
    return os.environ.get("ZAMANLANMIS_ACIK", "0").strip() == "1"


def _config_yolu() -> Path:
    """Config yolu: TRIGGER_CONFIG env'i, yoksa proje kökü tetikleyici.yaml.

    Go, komut env'inde TRIGGER_CONFIG'i her tetiklemede set eder (action.go).
    """
    if y := os.environ.get("TRIGGER_CONFIG", "").strip():
        return Path(y)
    return Path(__file__).resolve().parent.parent.parent / "tetikleyici.yaml"


def _ac_ayarla(acik: bool) -> str:
    """`zamanlanmis_mesaj.acik` değerini config dosyasında günceller.

    YAML'ı yeniden yazmaz; yalnız ilgili `acik:` satırını metin olarak
    değiştirir — yorumlar ve diğer yapılar aynen korunur. Atomik (tmp+rename).
    /zamanla aç|kapat (MEVZU Aşama 3, ADIM 2): engine her tetiklemede config'i
    yeniden okuduğundan (rules→zamanlanmisAcik) değişim restart'sız yansır.
    """
    deger = "true" if acik else "false"
    yol = _config_yolu()
    try:
        satirlar = yol.read_text(encoding="utf-8").splitlines()
    except OSError as e:
        return f"Config okunamadı: {e}"

    # zamanlanmis_mesaj: bloğunun nerede başladığını ve içindeki acik: satırını bul.
    blok_indis = None
    acik_indis = None
    for i, satir in enumerate(satirlar):
        cizgi = satir.strip()
        if cizgi.startswith("#"):
            continue
        if blok_indis is None and cizgi == "zamanlanmis_mesaj:":
            blok_indis = i
            continue
        if blok_indis is not None:
            if cizgi.startswith("acik:"):
                acik_indis = i
                break
            # Girinti azaldıysa blok bitti (üst seviye yeni anahtar).
            if cizgi and not satir.startswith((" ", "\t")):
                break

    if acik_indis is not None:
        girinti = satirlar[acik_indis][
            : len(satirlar[acik_indis]) - len(satirlar[acik_indis].lstrip())
        ]
        satirlar[acik_indis] = f"{girinti}acik: {deger}"
    elif blok_indis is not None:
        satirlar.insert(blok_indis + 1, f"  acik: {deger}")
    else:
        satirlar.append("")
        satirlar.append("zamanlanmis_mesaj:")
        satirlar.append(f"  acik: {deger}")

    gecici = yol.with_suffix(".yaml.tmp")
    try:
        gecici.write_text("\n".join(satirlar) + "\n", encoding="utf-8")
        gecici.rename(yol)
    except OSError as e:
        return f"Yazılamadı: {e}"
    if acik:
        return (
            "✅ zamanlanmış mesaj AÇILDI.\n"
            "Artık /zamanla <süre> <mesaj> kullanabilirsin.\n"
            "Kapatmak için: /zamanla kapat"
        )
    return "🛑 zamanlanmış mesaj KAPATILDI.\nAçmak için: /zamanla aç"


def _job_yol(dizin: Path, is_id: int) -> Path:
    return dizin / f"{is_id}.json"


def _yeni_id(dizin: Path) -> int:
    """Kısa sıralı iş numarası: kullanımda olmayan en küçük pozitif tam sayı.

    Eskiden `time.time_ns()` kullanılıyordu (19 haneli); sahibi sordu:
    "zamanlamaların idleri niye bu kadar uuzun" (2026-10-07). Kısa numara
    listede okunur, iptalde yazması kolay. Eski nano-ID dosyaları dururken
    bile 1, 2, 3… verilir (sayı olmayan dosyalar atlanır). `/reply` numara
    kuralıyla aynı mantık.
    """
    kullanilan = set()
    if dizin.is_dir():
        for yol in dizin.glob("*.json"):
            try:
                kullanilan.add(int(yol.stem))
            except ValueError:
                continue
    aday = 1
    while aday in kullanilan or (dizin / f"{aday}.json").exists():
        aday += 1
    return aday


def _isler(dizin: Path) -> list[dict]:
    if not dizin.is_dir():
        return []
    isler = []
    for yol in sorted(dizin.glob("*.json")):
        try:
            isler.append(json.loads(yol.read_text(encoding="utf-8")))
        except (OSError, json.JSONDecodeError):
            continue
    return isler


def _sure_coz(sure_metni: str) -> tuple[int | None, str | None]:
    """'10dk', '2sa', '1gun', '45sn' → saniye. Geçersizse (None, hata)."""
    sure_metni = sure_metni.strip().lower()
    m = SURE_PATTERN.match(sure_metni)
    if not m:
        return None, f"Süre anlaşılamadı: {sure_metni!r} (10dk, 2sa, 1gun, 45sn, 22:30)"
    sayi = int(m.group(1))
    saniye = sayi * _BIRIM_SANIYE[m.group(2)]
    if saniye < MIN_SANIYE:
        return None, "En az 1 dakika sonraysa zamanlanabilir."
    if saniye > MAX_SANIYE:
        return None, "En fazla 7 gün sonraya zamanlanabilir."
    return saniye, None


def _saat_coz(saat_metni: str) -> tuple[int | None, str | None]:
    """'22:30' → bir sonraki o saatteki Unix saniyesi."""
    m = SAAT_PATTERN.match(saat_metni.strip())
    if not m:
        return None, f"Saat anlaşılamadı: {saat_metni!r} (örn. 22:30)"
    saat, dakika = int(m.group(1)), int(m.group(2))
    if saat > 23 or dakika > 59:
        return None, "Saat aralık dışı: 00:00–23:59."
    simdi = datetime.now(_ISTANBUL)
    hedef = simdi.replace(hour=saat, minute=dakika, second=0, microsecond=0)
    if hedef <= simdi:
        hedef = hedef.replace(day=hedef.day + 1)
    fark = hedef.timestamp() - simdi.timestamp()
    if fark > MAX_SANIYE:
        return None, "En fazla 7 gün sonraya zamanlanabilir."
    return int(hedef.timestamp()), None


def _gosterim(fire_at: int) -> str:
    """Gönderim zamanını İstanbul saatine göre biçimlendirir."""
    yerel = datetime.fromtimestamp(fire_at, _ISTANBUL)
    kalan = fire_at - int(time.time())
    if kalan >= 3600:
        aralik = f"{kalan // 3600} sa {kalan % 3600 // 60} dk"
    else:
        aralik = f"{kalan // 60} dk"
    return f"{yerel.strftime('%H:%M')} ({aralik} sonra)"


def zamanla_komut(zaman_metni: str, metin: str, gonderen_numara: str) -> str:
    """/zamanla komutunu işler ve WhatsApp cevabını döndürür."""
    # MEVZU Aşama 3 (ADIM 2, 2026-10-06): /zamanla aç|kapat — çalışma anı
    # ayar kanalı. Kullanıcı config düzenlemeden açar/kapatır; engine her
    # tetiklemede config'i yeniden okuduğundan değişim restart'sız yansır.
    if zaman_metni in ("aç", "kapat"):
        return _ac_ayarla(zaman_metni == "aç")

    if not _acik_mi():
        return KAPALI_MESAJI

    dizin = _dizin()
    dizin.mkdir(parents=True, exist_ok=True)

    if zaman_metni == "list":
        isler = _isler(dizin)
        if not isler:
            return "📅 Bekleyen zamanlanmış mesaj yok."
        satirlar = ["📅 Zamanlanmış mesajlar:"]
        for is_ in isler:
            satirlar.append(
                f"#{is_.get('id')} · {_gosterim(int(is_['fire_at']))} · {is_['text']}"
            )
        satirlar.append("iptal için: /zamanla iptal <id>")
        return "\n".join(satirlar)

    if zaman_metni == "iptal":
        if not metin.isdigit():
            return "Hata: /zamanla iptal <id>"
        yol = _job_yol(dizin, int(metin))
        if yol.exists():
            yol.unlink()
            return f"🗑 Zamanlanmış mesaj #{metin} iptal edildi."
        return f"#{metin} bulunamadı."

    # Normal: <süre> <mesaj>
    if ":" in zaman_metni:
        fire_at, hata = _saat_coz(zaman_metni)  # mutlak zaman
    else:
        saniye, hata = _sure_coz(zaman_metni)  # göreli süre
        fire_at = int(time.time()) + saniye if saniye is not None else None
    if hata:
        return hata

    is_id = _yeni_id(dizin)  # kısa sıralı numara (1, 2, 3…)
    is_ = {
        "id": is_id,
        "to": gonderen_numara,
        "text": metin,
        "fire_at": fire_at,
        "created": int(time.time()),
        "deneme": 0,
        "son_deneme": 0,
    }
    yol = _job_yol(dizin, is_id)
    gecici = yol.with_suffix(".tmp")
    gecici.write_text(json.dumps(is_, ensure_ascii=False), encoding="utf-8")
    gecici.rename(yol)

    return (
        f"📅 Zamanlandı:\n"
        f"{metin}\n"
        f"⏰ Gönderim: {_gosterim(fire_at)}\n"
        f"iptal için: /zamanla iptal {is_id}"
    )


if __name__ == "__main__":
    import sys

    if len(sys.argv) >= 3:
        print(zamanla_komut(sys.argv[1], sys.argv[2], "test@lid"))
    else:
        print("kullanım: zamanla.py <süre|list|iptal> <mesaj>")
