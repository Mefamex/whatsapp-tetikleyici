#!/usr/bin/env python3
"""yetki.py — 3 aşamalı yetki modeli.

1. Kişi config'de var mı? (users listesinde number/name eşleşmesi)
2. O kişinin `/` ile gelen mesajlara yetkisi var mı? (allow_slash)
3. O kişinin `/cmd` özel yetkisi var mı? (allow_cmd)

YALNIZ config'deki users listesindeki kişiler yetkili sayılır.
Kural eşleşmesi (config'de var mı) = 1. adım zaten yapılmış olur.
"""

from __future__ import annotations

import os
import yaml
from pathlib import Path


def _config_yolu() -> str | None:
    """TRIGGER_CONFIG env veya varsayılan yol."""
    yol = os.getenv("TRIGGER_CONFIG", "").strip()
    if yol:
        return yol
    # cmd.py'nin yanından iki üst dizin = proje kökü
    return str(Path(__file__).resolve().parent.parent.parent / "tetikleyici.yaml")


def _users_yukle() -> list[dict]:
    """Config'ten users listesini yükle."""
    yol = _config_yolu()
    if not yol or not Path(yol).exists():
        return []
    try:
        with open(yol, encoding="utf-8") as f:
            veri = yaml.safe_load(f) or {}
        return veri.get("users", {}).get("users", [])
    except Exception:
        return []


def _eslesen_kullanici(number: str, name: str) -> dict | None:
    """Gelen numara/isimle eşleşen ilk kullanıcıyı bul."""
    users = _users_yukle()
    for u in users:
        nums = u.get("number", [])
        names = u.get("name", [])
        if number and number in nums:
            return u
        if name and name.lower() in (n.lower() for n in names):
            return u
    return None


def yetkili_mi(number: str, name: str) -> bool:
    """3 aşamalı yetki kontrolü:
    1. Kişi config'de var mı?
    2. / yetkisi var mı? (allow_slash)
    3. /cmd yetkisi var mı? (allow_cmd)
    """
    user = _eslesen_kullanici(number, name)
    if not user:
        return False
    # Aşama 2: / yetkisi
    if not user.get("allow_slash", False):
        return False
    # Aşama 3: /cmd yetkisi
    if not user.get("allow_cmd", False):
        return False
    return True


def yetki_detayli(number: str, name: str) -> dict:
    """Yetki detayını döndür (debug/log için)."""
    user = _eslesen_kullanici(number, name)
    if not user:
        return {"var_mi": False, "asama1": False, "asama2": False, "asama3": False}
    return {
        "var_mi": True,
        "asama1": True,  # config'de var
        "asama2": user.get("allow_slash", False),
        "asama3": user.get("allow_cmd", False),
        "izinler": {
            "allow_slash": user.get("allow_slash", False),
            "allow_cmd": user.get("allow_cmd", False),
            "allow_camera": user.get("allow_camera", False),
            "allow_durum": user.get("allow_durum", False),
            "allow_zamanla": user.get("allow_zamanla", False),
            "allow_reply": user.get("allow_reply", False),
        },
    }


def yetki_red_mesaji(number: str = "", name: str = "") -> str:
    """Yetkisiz erişim mesajı (detaylı)."""
    detay = yetki_detayli(number, name)
    if not detay["var_mi"]:
        return "Bu komutu çalıştırma yetkiniz yok: kullanıcı config'de tanımlı değil."
    if not detay["asama2"]:
        return "Bu komutu çalıştırma yetkiniz yok: / mesajları yetkisi yok."
    if not detay["asama3"]:
        return "Bu komutu çalıştırma yetkiniz yok: /cmd özel yetkisi yok."
    return "Bu komutu çalıştırma yetkiniz yok."


if __name__ == "__main__":
    # Test için config.yaml geçici yaz
    import tempfile

    test_yaml = """
users:
  users:
    - number: ["905550000001", "12345678901234@lid"]
      name: ["Mefamex"]
      allow_slash: true
      allow_cmd: true
      allow_camera: true
      allow_durum: true
      allow_zamanla: true
      allow_reply: true
"""
    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False) as f:
        f.write(test_yaml)
        test_config = f.name

    os.environ["TRIGGER_CONFIG"] = test_config

    testler = [
        ("905550000001", "Mefamex", True),
        ("12345678901234@lid", "Mefamex", True),
        ("905550000002", "Burak", False),
        ("", "Mefamex", False),  # numara gerekli
        ("905550000001", "", True),
        ("", "", False),
    ]

    for num, isim, beklenen in testler:
        sonuc = yetkili_mi(num, isim)
        durum = "✓" if sonuc == beklenen else "✗"
        print(f"{durum} yetkili_mi({num!r}, {isim!r}) = {sonuc} (beklenen: {beklenen})")

    # Detaylı test
    print("\nDetaylı:")
    print(yetki_detayli("905550000001", "Mefamex"))
    print(yetki_detayli("905550000002", "Burak"))
