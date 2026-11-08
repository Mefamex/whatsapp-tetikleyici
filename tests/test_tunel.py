#!/usr/bin/env python3
"""test_tunel.py — Tünel, /session ve /cmdhub komut testleri."""

import json
import os
import sys
import tempfile
from pathlib import Path

import pytest

# tetikleyici/cmd dizinini yola ekle
CMD_DIR = Path(__file__).resolve().parent.parent / "tetikleyici" / "cmd"
if str(CMD_DIR) not in sys.path:
    sys.path.insert(0, str(CMD_DIR))

from cmdhub import cmdhub_yonet, cmdhub_calistir
from session_manager import session_exit, session_go, session_listele, session_state, session_stop
from sozdizimi import parse_cmd
from tunel import (
    kullanici_modu,
    kullanici_tunel_al,
    kullanici_tunel_guncelle,
    kullanici_tunel_sifirla,
    tunel_dosya_yolu,
)


@pytest.fixture(autouse=True)
def gecici_state(monkeypatch):
    """Her test için geçici state dizini kullan."""
    with tempfile.TemporaryDirectory() as tmpdir:
        monkeypatch.setenv("TETIKLEYICI_STATE_DIR", tmpdir)
        yield tmpdir


def test_sozdizimi_session_komutlari():
    """Session ve cmdhub sözdizimi ayrıştırmaları doğru çalışmalı."""
    r = parse_cmd("/session list")
    assert r.komut_turu == "session_list"
    assert r.adet == 10

    r = parse_cmd("/session list 50")
    assert r.komut_turu == "session_list"
    assert r.adet == 50

    r = parse_cmd("/session go 2")
    assert r.komut_turu == "session_go"
    assert r.hedef == "2"

    r = parse_cmd("/session choose ses_12345")
    assert r.komut_turu == "session_go"
    assert r.hedef == "ses_12345"

    r = parse_cmd("/session exit")
    assert r.komut_turu == "session_exit"

    r = parse_cmd("/session stop")
    assert r.komut_turu == "session_stop"

    r = parse_cmd("/session state")
    assert r.komut_turu == "session_state"

    r = parse_cmd("/session status")
    assert r.komut_turu == "session_state"

    r = parse_cmd("/exit")
    assert r.komut_turu == "exit"

    r = parse_cmd("/cmdhub")
    assert r.komut_turu == "cmdhub"
    assert r.alt_komut == "on"

    r = parse_cmd("/cmdhub out")
    assert r.komut_turu == "cmdhub"
    assert r.alt_komut == "out"

    r = parse_cmd("/cmdhub cd /tmp")
    assert r.komut_turu == "cmdhub"
    assert r.alt_komut == "cd"
    assert r.hedef == "/tmp"

    r = parse_cmd("/cmdhub exit")
    assert r.komut_turu == "cmdhub"
    assert r.alt_komut == "exit"


def test_tunel_durumu_yonetimi():
    """Tünel durumu kaydetme ve sıfırlama çalışmalı."""
    jid = "12345678901234@lid"
    assert kullanici_modu(jid) == "normal"

    kullanici_tunel_guncelle(jid, mod="session", session_id="ses_test1", session_title="Test")
    assert kullanici_modu(jid) == "session"
    assert kullanici_tunel_al(jid)["session_id"] == "ses_test1"

    kullanici_tunel_sifirla(jid)
    assert kullanici_modu(jid) == "normal"
    assert kullanici_tunel_al(jid)["session_id"] == ""


def test_session_manager_flow(monkeypatch):
    """Session tüneline girme ve çıkma akışı."""
    jid = "12345678901234@lid"

    # Sahte session listesi mock'u
    sahte_sessionlar = [
        {"id": "ses_001", "title": "Birinci Oturum", "updated": 1794096000000},
        {"id": "ses_002", "title": "İkinci Oturum", "updated": 1794096500000},
    ]

    import session_manager
    monkeypatch.setattr(session_manager, "_opencode_session_listesi_al", lambda limit: (sahte_sessionlar, None))

    # 1. Listele
    cikti = session_listele(10)
    assert "Birinci Oturum" in cikti
    assert "ses_001" in cikti

    # 2. Go (1. session)
    go_cikti = session_go("1", jid)
    assert "Tüneli Açıldı" in go_cikti
    assert "ses_001" in go_cikti
    assert kullanici_modu(jid) == "session"

    # 3. State
    state_cikti = session_state(jid)
    assert "TÜNEL AKTİF" in state_cikti
    assert "ses_001" in state_cikti

    # 4. Exit
    exit_cikti = session_exit(jid)
    assert "çıkıldı" in exit_cikti
    assert kullanici_modu(jid) == "normal"


def test_cmdhub_flow():
    """CMDHUB tüneli ve komut çalıştırma akışı."""
    jid = "12345678901234@lid"

    # 1. CMDHUB aç
    res = cmdhub_yonet("on", "", jid)
    assert "CMDHUB Tüneli Açıldı" in res
    assert kullanici_modu(jid) == "cmdhub"

    # 2. Komut çalıştır
    res_cmd = cmdhub_calistir("echo 'cmdhub_test_123'", jid)
    assert "cmdhub_test_123" in res_cmd
    assert "Çıkış kodu: 0" in res_cmd

    # 3. /cmdhub out
    res_out = cmdhub_yonet("out", "", jid)
    assert "cmdhub_test_123" in res_out

    # 4. /cmdhub exit
    res_exit = cmdhub_yonet("exit", "", jid)
    assert "kapatıldı" in res_exit
    assert kullanici_modu(jid) == "normal"
