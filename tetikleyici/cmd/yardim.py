#!/usr/bin/env python3
"""yardim.py — /help metni."""

from __future__ import annotations


YARDIM_METNI = """/cmd — Komut yürütücü + Yardımcı komutlar

SÖZLEŞME
────────────────────
/cmd <kod>              → kod bir kez terminalde çalışır, çıktısı gelir
/cmd -<saniye> <kod>    → kod o kadar saniyede bir, kaldığı yerden devam ederek
                          çalışır; her turda çıktı gönderilir
/cmd stop [id]          → çalışan tekrarlayıcıyı durdur (id verilmezse tümü)
/cmd list               → aktif tekrarlayıcıları listele
/help                   → bu metni gösterir

YARDIMCI KOMUTLAR
────────────────────
/reply list             → bekleyen cevapları listele
/reply <n> <metin>      → n numaralı bekleyen cevap
/reply <metin>          → tek bekleyen varsa numarasız cevap
/zamanla <süre> <mesaj> → belirli süre/ zamanda mesaj gönder (örn: 10dk, 2sa, 22:30)
/zamanla list           → zamanlanmış mesajları listele
/zamanla iptal <id>     → zamanlanmış mesaj iptal
/camera                 → kameradan tek kare çekip gönderir (izinliyse)
/durum                  → tetikleyici + köprü anlık durumu (PID, uptime, kural, tampon)
/ping                   → pong (sağlık kontrolü)
/agent [motor]          → aktif ajan motorunu göster/değiştir (agy, claude, opencode)
/session list [n]       → son n session listesi (varsayılan 10)
/session go <n|id>      → session tüneline gir (tüm mesajlar o oturuma gider)
/session exit           → session tünelinden çık, ana oturuma dön
/session stop           → aktif session'ı durdur ve tünelden çık
/session state          → aktif session/tünel durumunu göster
/cmdhub                 → doğrudan bash kabuk tünel modunu aç
/cmdhub out             → son komutun tam çıktısını getir
/cmdhub cd <dizin>      → CMDHUB çalışma dizinini değiştir
/cmdhub exit            → CMDHUB modundan çık (veya /exit)

ÖRNEKLER
────────────────────
/cmd echo merhaba
/cmd ls -la /home/mfmx
/cmd -5 echo 'her 5 sn\'de bir'
/cmd -30 python3 script.py --arg değer
/cmd stop
/cmd stop 2
/cmd list
/reply 1 cevap metni
/zamanla 10dk kontrol mesajı
/agent agy
/agent durum
/session list
/session choose 1

ÇIKTI
────────────────────
• Her çalıştırma için **yeni zaman damgalı .txt** dosyası oluşturulur
• Klasör: `~/Desktop/whatsapp-cmd/`
• Format: `YYYYMMDD_HHMMSS_mmm.txt` (milisaniye hassasiyetli)
• Uzun çıktılarda **son 20 satır** WhatsApp'a gönderilir
• Tam çıktı dosyadan okunabilir
• Bittiğinde `cmd done` mesajı gönderilir

GÜVENLİK / YETKİ
────────────────────
• 3 aşamalı yetki kontrolü (config → / prefix → /cmd):
  1. Kişi config'de var mı? (mesaj dikkate alınır mı)
  2. `/` ile gelen mesajlara yetkisi var mı?
  3. `/cmd` özel yetkisi var mı?
• `allow_*` bayrakları 2-3. adımın kabuğu; `yetki.py` mefamex-dışını reddeder
• `sudo` **WhatsApp'tan geçmez** — gerekirse onay akışı ayrı süreç

SINIRLAR
────────────────────
• WhatsApp mesaj boyutu ~4096 karakter → son 20 satır gönderilir
• Çıktı dosyası 50KB'tan büyükse kırpılır
• Zaman aşımı: varsayılan 3 dakika (komut bazlı ayarlanabilir)
• Tekrarlayıcı minimum 1 saniye aralık
• Aynı anda çok fazla tekrarlayıcı sistem yüklenir (makul tutun)

HEARTBEAT (OTOMATİK)
────────────────────
• Sohbet 15 dk sessiz kalırsa: "admin ile konuşman bir süredir durağan.
  ona mesaj atman gerekiyorsa at" → ajana gider, cevap sohbete düşer
• Kontrol: her 1 dk; aktif sohbette tetiklenmez (döngü kendiliğinden kırılır)

ZAMANLAYICI (OPENCODE-ZAMANLAYICI)
────────────────────
• Python scheduler (calistir.py) **while-true loop** içinde çalışır
• PID: `/tmp/zamanlayici_loop.pid` → `kill $(cat /tmp/zamanlayici_loop.pid)` ile durdur
• Watchdog: her 60sn session boşluk kontrolü, 10dk pencerede 1× "devam et"
• Brifing: 3600sn (1 saat) arayla tam rapor → WhatsApp özeti
• Config: `/home/mfmx/Desktop/opencode-zamanlayici/config.yaml`
• Auto-restart: `run_loop.sh` (kapanırsa 2sn sonra yeniden başlar)

NOTLAR
────────────────────
• "Kaldığı yerden devam" = aynı komut her turda baştan çalışır,
  ama çıktı dosyası **ekleme modunda** büyür (geçmiş birikir)
• Durdurma: `/cmd stop` (tümü) veya `/cmd stop <id>` (tek) — child PID doğrudan öldürülür
• Liste: `/cmd list` → ID, komut, aralık, tur sayısı, dosya adı
• Bu komut **agent kullanmaz** — stdout doğrudan cevaptır
• Zamanlanmış mesajlar `zamanlanmis_mesaj.acik: true` iken çalışır (restart'a dayanır)
• `/session` komutları opencode API'si kullanır (port 4096)
"""


def yardim_al() -> str:
    """Yardım metnini döndür."""
    return YARDIM_METNI


def kisa_yardim() -> str:
    """Kısa yardım (WhatsApp için)."""
    return (
        "/cmd komutları:\n"
        "  /cmd <kod>           → bir kez çalıştır\n"
        "  /cmd -<sn> <kod>     → her N saniyede bir tekrarla\n"
        "  /cmd stop [id]       → durdur\n"
        "  /cmd list            → listele\n"
        "  /reply list          → bekleyen cevaplar\n"
        "  /reply <n> <metin>   → n numaralı cevap\n"
        "  /zamanla <süre> <msj> → planla\n"
        "  /zamanla list        → listele\n"
        "  /camera              → kamera kare (izinliyse)\n"
        "  /durum               → tetikleyici + köprü durumu\n"
        "  /ping                → pong\n"
        "  /agent [motor]       → ajan motoru (agy, claude, opencode)\n"
        "  /session list [n]    → session'lar (go <n>, exit, stop, state)\n"
        "  /cmdhub              → kabuk tüneli (out, cd, exit)\n"
        "  /exit                → aktif tünelden çık\n"
        "  /help                → bu yardım\n\n"
        "Sadece mefamex. Çıktı ~/Desktop/whatsapp-cmd/."
    )


if __name__ == "__main__":
    print(YARDIM_METNI)
