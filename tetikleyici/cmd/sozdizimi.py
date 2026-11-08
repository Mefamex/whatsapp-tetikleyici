#!/usr/bin/env python3
"""sozdizimi.py — /cmd ve /session sözdizimi ayrıştırma.

Desteklenen biçimler:
    /cmd <kod>                    → bir kez çalıştır
    /cmd -<saniye> <kod>          → her N saniyede bir çalıştır (tekrarlayan)
    /cmd stop                     → çalışan tekrarlayıcıyı durdur
    /cmd list                     → aktif tekrarlayıcıları listele
    /session list                 → opencode session'ları listele (admin)
    /session choose <numara>      → session seç (admin)
    /zamanla <süre> <mesaj>       → zamanlanmış mesaj (10dk, 2sa, 22:30…)
    /zamanla list                 → bekleyen zamanlanmış mesajları listele
    /zamanla iptal <id>           → zamanlanmış mesajı iptal et
    /zamanla aç | kapat           → çalışma anında aç/kapat (config'siz)
    /reply <cevap>                → bekleyen soruya cevap (numarasız, 0'a gider)
    /reply <numara> <cevap>       → belirli bekleyen soruya cevap
    /reply list                   → bekleyen soru numaralarını göster
    /ping                         → pong (köprü/tetikleme sağlığı, ajan gerekmez)
    /help                         → yardım metni
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Optional


@dataclass
class CmdParseResult:
    """Ayrıştırma sonucu."""

    komut_turu: str  # "once", "repeat", "stop", "list", "help", "camera", "durum", "reply", "call", "agent", "session_list", "session_go", "session_exit", "session_stop", "session_state", "cmdhub", "exit", "invalid"
    kod: str = ""
    saniye: int = 0
    hata: str = ""
    # /zamanla için: zaman_metni süre/list/iptal, metin ise gönderilecek yazı.
    zaman_metni: str = ""
    metin: str = ""
    # /reply için: numara "3" | "list" | "" (numarasız). metin = cevap.
    numara: str = ""
    # /session için adet ve hedef (go/choose için)
    adet: int = 10
    hedef: str = ""
    # /cmdhub için alt komut
    alt_komut: str = ""


CMD_PATTERN = re.compile(r"^/cmd\s+(?P<arg>.+)$", re.IGNORECASE)
STOP_PATTERN = re.compile(r"^/cmd\s+stop\s*(?P<id>\d+)?$", re.IGNORECASE)
LIST_PATTERN = re.compile(r"^/cmd\s+list\s*$", re.IGNORECASE)
SESSION_LIST_PATTERN = re.compile(r"^/session\s+list(?:\s+(?P<adet>\d+))?\s*$", re.IGNORECASE)
SESSION_GO_PATTERN = re.compile(
    r"^/session\s+(?:go|choose)\s+(?P<hedef>\S+)\s*$", re.IGNORECASE
)
SESSION_EXIT_PATTERN = re.compile(r"^/session\s+exit\s*$", re.IGNORECASE)
SESSION_STOP_PATTERN = re.compile(r"^/session\s+stop\s*$", re.IGNORECASE)
SESSION_STATE_PATTERN = re.compile(r"^/session\s+(?:state|status)\s*$", re.IGNORECASE)
CMDHUB_PATTERN = re.compile(r"^/cmdhub(?:\s+(?P<arg>.+))?$", re.IGNORECASE)
EXIT_PATTERN = re.compile(r"^/exit\s*$", re.IGNORECASE)
HELP_PATTERN = re.compile(r"^/help\s*$", re.IGNORECASE)
CAMERA_PATTERN = re.compile(r"^/camera\s*$", re.IGNORECASE)
DURUM_PATTERN = re.compile(r"^/durum\s*$", re.IGNORECASE)
PING_PATTERN = re.compile(r"^/ping\s*$", re.IGNORECASE)
REVOKE_PATTERN = re.compile(r"^/(?:sil|revoke)\b", re.IGNORECASE)
AGENT_PATTERN = re.compile(r"^/agent(?:\s+(?P<arg>.+))?$", re.IGNORECASE)
CALL_PATTERN = re.compile(r"^/call\s+(?P<arg>.+)$", re.IGNORECASE)
ZAMANLA_PATTERN = re.compile(r"^/zamanla\s+(?P<arg>.+)$", re.IGNORECASE)
ZAMANLA_IPTAL_PATTERN = re.compile(r"^iptal\s+(?P<id>\d+)\s*$", re.IGNORECASE)
REPLY_PATTERN = re.compile(r"^/reply\s+(?P<arg>.+)$", re.IGNORECASE)


def parse_cmd(metin: str) -> CmdParseResult:
    """Mesaj metnini /cmd veya /session komutu olarak ayrıştır."""
    metin = metin.strip()

    # /help
    if HELP_PATTERN.match(metin):
        return CmdParseResult(komut_turu="help")

    # /camera
    if CAMERA_PATTERN.match(metin):
        return CmdParseResult(komut_turu="camera")

    # /durum
    if DURUM_PATTERN.match(metin):
        return CmdParseResult(komut_turu="durum")

    # /ping — bağlantı/tetikleme sağlığı (ajan turuna gerek yok)
    if PING_PATTERN.match(metin):
        return CmdParseResult(komut_turu="ping")

    # /sil veya /revoke — alıntılanan mesajı herkesten sil
    if REVOKE_PATTERN.match(metin):
        parcalar = metin.split(None, 1)
        hedef_id = parcalar[1].strip() if len(parcalar) > 1 else ""
        return CmdParseResult(komut_turu="revoke", hedef=hedef_id)

    # /agent [agy|claude|opencode|durum]
    agent_match = AGENT_PATTERN.match(metin)
    if agent_match:
        arg = (agent_match.group("arg") or "").strip()
        if not arg or arg.lower() in ("durum", "status", "list", "state"):
            return CmdParseResult(komut_turu="agent", kod="status")
        motor = arg.lower()
        if motor in ("agy", "claude", "opencode"):
            return CmdParseResult(komut_turu="agent", kod=motor)
        return CmdParseResult(
            komut_turu="invalid",
            hata="Geçersiz ajan motoru. Kullanım: /agent [agy|claude|opencode|durum]",
        )

    # /call <jid> [süre] — giden arama (çaldırma)
    call_match = CALL_PATTERN.match(metin)
    if call_match:
        arg = call_match.group("arg").strip()
        parca = arg.split()
        if len(parca) >= 1:
            jid = parca[0]
            sure = 30
            if len(parca) >= 2 and parca[1].isdigit():
                sure = int(parca[1])
            return CmdParseResult(komut_turu="call", kod=jid, saniye=sure)
        return CmdParseResult(komut_turu="invalid", hata="/call <jid> [süre_saniye]")

    # /zamanla list | iptal <id> | <süre> <mesaj>
    zamanla_match = ZAMANLA_PATTERN.match(metin)
    if zamanla_match:
        arg = zamanla_match.group("arg").strip()
        if arg.lower() in ("list", "liste"):
            return CmdParseResult(komut_turu="zamanla", zaman_metni="list")
        # Aşama 3 (2026-10-06): /zamanla aç|kapat — çalışma anı ayar kanalı.
        # Kullanıcı config düzenlemeden özelliği açıp kapatabilir.
        if arg.lower() in ("aç", "ac", "kapat"):
            return CmdParseResult(komut_turu="zamanla", zaman_metni=arg.lower())
        iptal = ZAMANLA_IPTAL_PATTERN.match(arg)
        if iptal:
            return CmdParseResult(
                komut_turu="zamanla", zaman_metni="iptal", metin=iptal.group("id")
            )
        parca = arg.split(None, 1)
        if len(parca) == 2 and parca[0]:
            return CmdParseResult(
                komut_turu="zamanla", zaman_metni=parca[0], metin=parca[1].strip()
            )
        return CmdParseResult(
            komut_turu="invalid",
            hata="/zamanla <süre> <mesaj> | list | iptal <id>",
        )

    # /exit — aktif tünelden (session veya cmdhub) hızlı çıkış
    if EXIT_PATTERN.match(metin):
        return CmdParseResult(komut_turu="exit")

    # /session list [adet]
    session_list_match = SESSION_LIST_PATTERN.match(metin)
    if session_list_match:
        adet_str = session_list_match.group("adet")
        adet = int(adet_str) if adet_str else 10
        return CmdParseResult(komut_turu="session_list", adet=adet)

    # /session go <hedef> veya /session choose <hedef>
    session_go_match = SESSION_GO_PATTERN.match(metin)
    if session_go_match:
        hedef = session_go_match.group("hedef")
        return CmdParseResult(komut_turu="session_go", hedef=hedef, kod=hedef)

    # /session exit
    if SESSION_EXIT_PATTERN.match(metin):
        return CmdParseResult(komut_turu="session_exit")

    # /session stop
    if SESSION_STOP_PATTERN.match(metin):
        return CmdParseResult(komut_turu="session_stop")

    # /session state / status
    if SESSION_STATE_PATTERN.match(metin):
        return CmdParseResult(komut_turu="session_state")

    # /cmdhub [alt_komut]
    cmdhub_match = CMDHUB_PATTERN.match(metin)
    if cmdhub_match:
        arg = (cmdhub_match.group("arg") or "").strip()
        if not arg or arg.lower() in ("on", "enter", "start"):
            return CmdParseResult(komut_turu="cmdhub", alt_komut="on")
        if arg.lower() in ("exit", "off", "stop"):
            return CmdParseResult(komut_turu="cmdhub", alt_komut="exit")
        if arg.lower() in ("out", "output"):
            return CmdParseResult(komut_turu="cmdhub", alt_komut="out")
        if arg.lower() in ("state", "status"):
            return CmdParseResult(komut_turu="cmdhub", alt_komut="state")
        if arg.lower().startswith("cd "):
            return CmdParseResult(komut_turu="cmdhub", alt_komut="cd", hedef=arg[3:].strip())
        return CmdParseResult(komut_turu="cmdhub", alt_komut="exec", kod=arg)

    # /reply list | <numara> <cevap> | <cevap>
    reply_match = REPLY_PATTERN.match(metin)
    if reply_match:
        arg = reply_match.group("arg").strip()
        if arg.lower() in ("list", "liste"):
            return CmdParseResult(komut_turu="reply", numara="list")
        parca = arg.split(None, 1)
        if len(parca) == 2 and parca[0].isdigit():
            return CmdParseResult(
                komut_turu="reply", numara=parca[0], metin=parca[1].strip()
            )
        return CmdParseResult(komut_turu="reply", numara="", metin=arg)

    # /cmd list
    if LIST_PATTERN.match(metin):
        return CmdParseResult(komut_turu="list")

    # /cmd stop [id]
    stop_match = STOP_PATTERN.match(metin)
    if stop_match:
        id_str = stop_match.group("id")
        if id_str:
            return CmdParseResult(komut_turu="stop", kod=id_str)
        return CmdParseResult(komut_turu="stop_all")

    # /cmd <kod> veya /cmd -<saniye> <kod>
    match = CMD_PATTERN.match(metin)
    if not match:
        return CmdParseResult(komut_turu="invalid", hata="Geçersiz /cmd biçimi")

    arg = match.group("arg").strip()

    # -<saniye> kontrolü
    repeat_match = re.match(r"^-(\d+)\s+(.+)$", arg)
    if repeat_match:
        saniye = int(repeat_match.group(1))
        kod = repeat_match.group(2).strip()
        if saniye <= 0:
            return CmdParseResult(komut_turu="invalid", hata="Süre 0'dan büyük olmalı")
        if not kod:
            return CmdParseResult(komut_turu="invalid", hata="Çalıştırılacak kod boş")
        return CmdParseResult(komut_turu="repeat", kod=kod, saniye=saniye)

    # Tek seferlik
    if not arg:
        return CmdParseResult(komut_turu="invalid", hata="Çalıştırılacak kod boş")

    # Sadece "-N" (kod yok) kontrolü - repeat pattern'de kod boş kalır
    if re.match(r"^-\d+$", arg):
        return CmdParseResult(komut_turu="invalid", hata="Çalıştırılacak kod boş")

    return CmdParseResult(komut_turu="once", kod=arg)


if __name__ == "__main__":
    # Testler
    testler = [
        ("/cmd echo merhaba", "once", "echo merhaba", 0),
        ("/cmd -5 echo merhaba", "repeat", "echo merhaba", 5),
        ("/cmd -30 python3 script.py", "repeat", "python3 script.py", 30),
        ("/cmd stop", "stop_all", "", 0),
        ("/cmd stop 2", "stop", "2", 0),
        ("/cmd list", "list", "", 0),
        ("/session list", "session_list", "", 0),
        ("/session list 50", "session_list", "", 0),
        ("/session go 3", "session_go", "3", 0),
        ("/session choose 3", "session_go", "3", 0),
        ("/session exit", "session_exit", "", 0),
        ("/session stop", "session_stop", "", 0),
        ("/session state", "session_state", "", 0),
        ("/cmdhub", "cmdhub", "", 0),
        ("/cmdhub out", "cmdhub", "", 0),
        ("/cmdhub exit", "cmdhub", "", 0),
        ("/exit", "exit", "", 0),
        ("/camera", "camera", "", 0),
        ("/durum", "durum", "", 0),
        ("/help", "help", "", 0),
        ("/cmd", "invalid", "", 0),
        ("/cmd -0 echo", "invalid", "", 0),
        ("/cmd -5", "invalid", "", 0),
    ]

    for metin, beklenen_tur, beklenen_kod, beklenen_saniye in testler:
        sonuc = parse_cmd(metin)
        ok = (
            sonuc.komut_turu == beklenen_tur
            and sonuc.kod == beklenen_kod
            and sonuc.saniye == beklenen_saniye
        )
        durum = "✓" if ok else "✗"
        print(
            f"{durum} {metin!r} → tur={sonuc.komut_turu}, kod={sonuc.kod!r}, sn={sonuc.saniye}, hata={sonuc.hata!r}"
        )

    # /zamanla ayrıştırma (zaman_metni + metin alanlarıyla)
    zamanla_testler = [
        ("/zamanla 10dk su iç", "zamanla", "10dk", "su iç"),
        ("/zamanla 22:30 hatırlat", "zamanla", "22:30", "hatırlat"),
        ("/zamanla list", "zamanla", "list", ""),
        ("/zamanla iptal 3", "zamanla", "iptal", "3"),
        # Aşama 3: /zamanla aç|kapat çalışma anı ayar kanalı
        ("/zamanla aç", "zamanla", "aç", ""),
        ("/zamanla kapat", "zamanla", "kapat", ""),
        ("/zamanla AC", "zamanla", "ac", ""),
        ("/zamanla sadece-sure", "invalid", "", ""),
    ]
    for metin, beklenen_tur, beklenen_zaman, beklenen_metin in zamanla_testler:
        sonuc = parse_cmd(metin)
        ok = (
            sonuc.komut_turu == beklenen_tur
            and sonuc.zaman_metni == beklenen_zaman
            and sonuc.metin == beklenen_metin
        )
        durum = "✓" if ok else "✗"
        print(
            f"{durum} {metin!r} → tur={sonuc.komut_turu}, zaman={sonuc.zaman_metni!r}, metin={sonuc.metin!r}"
        )

    # /reply ayrıştırma (numara + metin alanlarıyla)
    reply_testler = [
        ("/reply 3 cevap burada", "reply", "3", "cevap burada"),
        ("/reply cevap burada", "reply", "", "cevap burada"),
        ("/reply list", "reply", "list", ""),
        ("/reply 7", "reply", "", "7"),  # tek kelime → numarasız cevap
    ]
    for metin, beklenen_tur, beklenen_numara, beklenen_metin in reply_testler:
        sonuc = parse_cmd(metin)
        ok = (
            sonuc.komut_turu == beklenen_tur
            and sonuc.numara == beklenen_numara
            and sonuc.metin == beklenen_metin
        )
        durum = "✓" if ok else "✗"
        print(
            f"{durum} {metin!r} → tur={sonuc.komut_turu}, numara={sonuc.numara!r}, metin={sonuc.metin!r}"
        )
