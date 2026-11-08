#!/usr/bin/env python3
"""/agent komutu testleri (sozdizimi + cmd.py)."""

import importlib.util
import os
import shutil
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

CMD_DIR = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "tetikleyici", "cmd"
)
sys.path.insert(0, CMD_DIR)

from sozdizimi import parse_cmd  # noqa: E402

_spec = importlib.util.spec_from_file_location(
    "cmd_modulu", os.path.join(CMD_DIR, "cmd.py")
)
cmd_py = importlib.util.module_from_spec(_spec)
assert _spec.loader is not None
_spec.loader.exec_module(cmd_py)

SAHIP = "12345678901234@lid"


class AgentKomutTestleri(unittest.TestCase):
    def setUp(self):
        self.tmpdir = tempfile.mkdtemp()
        os.environ["TETIKLEYICI_STATE_DIR"] = self.tmpdir

    def tearDown(self):
        shutil.rmtree(self.tmpdir, ignore_errors=True)
        os.environ.pop("TETIKLEYICI_STATE_DIR", None)

    def test_parse_agent_durum(self):
        for metin in ("/agent", "/agent durum", "/agent status", "  /agent  "):
            s = parse_cmd(metin)
            self.assertEqual(s.komut_turu, "agent")
            self.assertEqual(s.kod, "status")

    def test_parse_agent_secim(self):
        for motor in ("agy", "claude", "opencode"):
            s = parse_cmd(f"/agent {motor}")
            self.assertEqual(s.komut_turu, "agent")
            self.assertEqual(s.kod, motor)

    def test_parse_agent_gecersiz(self):
        s = parse_cmd("/agent gecersiz_motor")
        self.assertEqual(s.komut_turu, "invalid")

    def test_agent_durum_varsayilan_agy(self):
        yanit = cmd_py.ana("/agent", SAHIP, "Mefamex")
        self.assertIn("Aktif Ajan Motoru: *agy*", yanit)
        self.assertIn("agy (aktif) ✅", yanit)

    def test_agent_motor_degistir(self):
        yanit = cmd_py.ana("/agent claude", SAHIP, "Mefamex")
        self.assertIn("Aktif ajan motoru değiştirildi: *claude*", yanit)

        # Durum kontrol et
        yanit_durum = cmd_py.ana("/agent", SAHIP, "Mefamex")
        self.assertIn("Aktif Ajan Motoru: *claude*", yanit_durum)
        self.assertIn("claude (aktif) ✅", yanit_durum)

        # Dosyayı doğrula
        state_file = os.path.join(self.tmpdir, "aktif_motor.txt")
        self.assertTrue(os.path.isfile(state_file))
        with open(state_file, encoding="utf-8") as f:
            self.assertEqual(f.read().strip(), "claude")


if __name__ == "__main__":
    unittest.main()
