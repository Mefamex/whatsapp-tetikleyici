#!/usr/bin/env python3
"""/ping komutu testleri (sozdizimi + cmd.py).

CANLI GÖZLEM (2026-10-06 23:28): sahip `/ping` yazdı, "Hata: Geçersiz
/cmd biçimi" döndü — `/ping` hiç tanımlı değildi (config yorumu "/cmd, /ping"
diyordu ama handler yoktu). Küçük harfli "ping" ajana gidip 17 sn'de pong
dönüyordu; slash'lı komut anında yanıt üretmeli.

Çalıştırma:
    python3 -m unittest discover -s tests -p 'test_ping*' -v
"""

import importlib.util
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

CMD_DIR = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "tetikleyici", "cmd"
)
sys.path.insert(0, CMD_DIR)

# ⚠️ `import cmd` KULLANMA: stdlib'de de `cmd` var; pytest onu önceden içe
# aktarıyor ve `import cmd` bizim dosyayı DEĞİL stdlib'i döndürüyor.
# (unittest'te aradaki fark görünmez: sıra şansına bizimki kazanıyordu.)
from sozdizimi import parse_cmd  # noqa: E402

_spec = importlib.util.spec_from_file_location(
    "cmd_modulu", os.path.join(CMD_DIR, "cmd.py")
)
cmd_py = importlib.util.module_from_spec(_spec)
assert _spec.loader is not None
_spec.loader.exec_module(cmd_py)

SAHIP = "12345678901234@lid"


class PingKomutTestleri(unittest.TestCase):
    def test_parse_ping(self):
        s = parse_cmd("/ping")
        self.assertEqual(s.komut_turu, "ping")

    def test_parse_ping_buyuk_kucuk(self):
        s = parse_cmd("/PING")
        self.assertEqual(s.komut_turu, "ping")

    def test_parse_ping_boşluk_tolerans(self):
        s = parse_cmd("  /ping  ")
        self.assertEqual(s.komut_turu, "ping")

    def test_cmd_ana_pong(self):
        cevap = cmd_py.ana("/ping", SAHIP, "Mefamex")
        self.assertEqual(cevap, "pong 🏓")

    def test_cmd_ana_yetkisiz_reddeder(self):
        # /ping de diğer komutlar gibi yetki katmanından geçer.
        cevap = cmd_py.ana("/ping", "905599999999@s.whatsapp.net", "Yabancı")
        self.assertIn("yetk", cevap.lower())


if __name__ == "__main__":
    unittest.main()
