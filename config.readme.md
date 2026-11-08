# config.readme.md — `tetikleyici.yaml` kılavuzu

> **Bu dosya, `tetikleyici.yaml`'ın açıklamasıdır.** Config kısa tutulur;
> burada duran ayrıntılar oraya taşınır. Config'de değişiklik yaparken
> burayı da güncelle.

`tetikleyici.yaml` her mesajda yeniden okunur. Düzenle, kaydet, bitti —
**restart yok, servis yeniden başlatma yok.**

```bash
./tetikleyici.sh start      # başlat
./tetikleyici.sh status     # durum
./tetikleyici.sh log 50     # son 50 satır günlük
```

## ⛔ Sessizlik kasıtlıdır — otomatik cevap konulmaz

2026-10-06'da tetikleyici yalnız mefamex'e yanıt verecek şekilde
daraltıldı. Kapalı hatlara **hiçbir otomatik cevap verilmez**:

- ✗ "şu an kapalıyım" · ✗ "mevcut değil" · ✗ "mefamex'e yaz"
- ✗ ping'e karşılık · ✗ hiçbir sabit metin

Bunu **önerdim, kullanıcı kesinlikle reddetti**:
*"kesinlikle böyle bişe yok. kapalıysa kapalı bunu kritik olarak not et."*

**Neden:** kapalı olmanın anlamı, o hatta giren hiçbir şey olmaması.
Bir cevap hattın çalıştığını belli eder; kapalı hat sessizdir. Sessiz
cevap, olmayan bir hattı varmış gibi gösterir.

**Yanlış düşünce:** *"sessizce kaybolmak kötü, en azından bir şey söyleyeyim"*
→ karşıtı doğru. Sessizlik de bir davranıştır, gürültü değil.

Kayıt: `NOTES.md` madde 31.

---

## 1. `match` koşulları

Hepsi birden tutmalı (**AND**), `number`/`name` çifti hariç.

| Koşul | Örnek | Anlamı |
|---|---|---|
| `number` | `905551234567` | sohbet kimliği (tek değer veya liste) |
| `wp_number` | `905551234567` | `number`'ın **okunur adı** (WhatsApp number) |
| `name` | `Mefamex` | kişi adı (`push_name`) |
| `wp_name` | `Mefamex` | `name`'in **okunur adı** (WhatsApp name) |
| `text` | `müfredat` | mesaj metni **içeriyor** |
| `not_text` | `ping` | bu kelime geçiyorsa kural **TUTMAZ** |
| `type` | `audio` | mesajın **TİPİ** |
| `group` | `true` | yalnız gruplar |
| `everyone` | `true` | koşulsuz, her mesaj |

### `wp_number` / `wp_name` neden var?

`number` ve `name` **beklenmedik** yerlerde kullanılıyordu: `number` hem
WhatsApp numarası hem de sohbet kimliği (JID) anlamına geliyordu,
`name` ise push_name ile birlikte "hat adı" anlamına da kayıyordu.
Sonuç: başka bir sistemle (WhatsApp Business API) konuşan biri bu alanları
görünce neyi tuttuğunu tahmin edemiyor.

`wp_number` ve `wp_name` aynı şeyi **okunur adıyla** söyler ve
`number`/`name` ile birleştirilir — ikisinden birini yazmak yeterli:

```
number  ∪ wp_number  →  "Numbers"
name    ∪ wp_name    →  "Names"
```

> ⛔ Bu bir **ayrı koşul değil**, bir **adlandırma**. `wp_number` yazıp
> `number`'ı silmek davranışı değiştirmez.

### İki tuzak

**1) LID türetilemez.** Telefon `905551234567` → LID `12345678901234`
**tamamen başka bir sayı**. LID'li sohbeti *sadece* `number` ile
yakalayamazsın — ikisini birden yaz.

**2) Türkçe karakter.** `'müfredat'` yazarsan `'mufredat'` **TUTMAZ**.

### Mesaj tipleri (köprü bu adları doğrudan verir)

```
audio · image · video · document · text · sticker
```

`'audio'` yaz — `'voice'`, `'ptt'`, `'sound'` **TUTMAZ**.

### `number` / `name` öncelikli OR'dur

Önce `number` denenir; tutmazsa `name`'e düşülür. Günlükte bu
`name=Mefamex (fallback)` olarak görünür — normal, hata değil.

---

## 2. `do` eylemleri

Sırayla çalışır; biri çökse diğerleri de çalışır.

| `type` | Alanlar | Ne yapar |
|---|---|---|
| `command` | `code`, `reply`, `agent`, `timeout` | `sh -c` ile çalıştırır |
| `session` | `session`, `reply`, `agent`, `timeout` | `opencode run --session <id>` |
| `new_session` | `reply`, `agent`, `timeout` | her seferinde yeni oturum |
| **`opencode`** | `cmd`, `gorsel`, `ses`, `allow_*`, `send_seen`, `add_aitag`, `do_debug` | **mesajın tipine göre doğru hatta yönlendirir** (§ 2.1) |

| Alan | Değer | Anlamı |
|---|---|---|
| `reply` | `true` | çıktıyı sohbete gönder |
| `reply` | `"sabit metin"` | çıktıyı yok say, bunu gönder |
| `agent` | `wp-sahip` | ajan oturumu açılır. Verilmezse stdout **doğrudan** cevaptır |
| `timeout` | `12m` | tek eylemin üst sınırı — **kural > bayrak > varsayılan (3m)** |

> `timeout` birimsiz sayı verilirse (`timeout: 15`) sıfıra düşmez,
> günlüğe yazılır.

### `agent` vermek neyi değiştirir?

Ajan **yorumlar, araştırır, araç çağırır** — stdout'u olduğu gibi
göndermez. Bir de dosya/görsel **gönderebilir**; bu yetenek ajanın
`whatsapp_send_media` aracındadır.

`gorsel_pipeline.py` bu yüzden `agent: wp-sahip` ile çağrılır.

---

## 2.1 `type: opencode` — tek kişi tek kural

Bir kişi için beş ayrı kural yazmak yerine **tek kural** yazıp
yönlendirmeyi eylemin kendisine bırakır.

**Neden:** beş kural, aynı numarayı beş kez yazmak demekti; birinden
birini silmek diğerlerinin eşleşmesini bozuyordu ve "hangi kural ne işe
yarıyordu" sorusu her seferinde yeniden cevaplanıyordu.

```yaml
  - name: mefamex
    match:
      wp_number: ["905551234567", "12345678901234@lid"]
      wp_name: Mefamex
    do:
      - type: opencode       # oturum + hat yönlendirmesi, tek eylemde
        session: ses_ef8590662ffeRSLXLL2R8VhJfM   # ⛔ ZORUNLU
        agent: wp-sahip      # gönderim aracı kapalı ajan → çift gönderim olmaz
        reply: true
        timeout: 12m
        cmd:      ~/bin/cmd.py
        gorsel:   ~/Desktop/whatsapp-bridge-project/gorsel_pipeline.py
        ses:      ~/Desktop/whatsapp-bridge-project/voice_pipeline.py
        send_seen: true      # bu kural için mavi tik
        add_aitag: false     # bu kural için etiket yok
        do_debug: false      # bu kural için ara mesaj yok
        allow_help: true
        allow_cmd: true
        allow_durum: true
        allow_camera: true
        allow_gorsel: true
        allow_ses: true
        allow_belge: true
        allow_reaction: true
```

> `session` **zorunlu**: `opencode` eylemi her yazılı mesajı o oturuma
> verir. `opencode run --session` olmadan düşünmez.

### Hat haritası

| mesaj tipi | → hat | izin |
|---|---|---|
| `text`, `extended_text`, `poll` | opencode oturumu | (her zaman) |
| `reaction` (emoji) | opencode oturumu | `allow_reaction` |
| `text` `/help` | `cmd` | `allow_help` |
| `text` `/durum` | `cmd` | `allow_durum` |
| `text` `/camera` | `cmd` | `allow_camera` |
| `text` `/zamanla …` | `cmd` | `allow_cmd` / `allow_durum` |
| `text` başka `/…` | `cmd` | `allow_cmd` |
| `image`, `video`, `sticker` | `gorsel` | `allow_gorsel` |
| `document` | `gorsel` | `allow_belge` |
| `audio` | `ses` | `allow_ses` |
| `protocol`, `poll_update`, `noPayload`, `unknown` | — (sessiz) | — |

> ⛔ **`/` ile başlayan metin AJANA GİTMEZ.** Komut hattına gider. İzni
> kapalıysa da ajana düşmez: "kapalı" demek "sessiz" demektir.

> ⛔ **En az bir `allow_*` yazılmalı.** `type: opencode` yalnız izin
> bayraklarıyla anlam kazanır; hepsi kapalıysa eylem hiçbir işe yaramaz
> ve bu bir hatadır (config yüklenmez).

### İzin kuralının tam metni

Yukarıdaki tabloların hepsini tek cümleye indiren kural — aranacaksa
burada aranır:

> **Bir yetenek `allow_*` ile kapalıysa, o yeteneğe düşen mesaj hiçbir
> şeye dönüşmez: ne cevap gider, ne ajan çağrılır, ne hata yazılır, ne
> "çıktı boş" bildirimi gider — ve mesaj OKUNDU da işaretlenmez.**
>
> Doğrulandı (`action.go`, `yonlendir` → `if !varMi { return "", nil }`):
> akış, hat seçilmeden **çıkıyor**. `sendSeenIfAsked` çağrısı ondan
> **sonra** geliyor, yani kapalı hatta mavi tik bile basılmaz.

Bunun tek izi günlüktedir:

```
yön yok (image tipi, izin kapalı) → sessizce geçildi
```

Bu kural, dokümanın başındaki *"kapalı hat sessizdir"* ilkesinin **kardeşidir**:

| | neyi kapsar | sonuç |
|---|---|---|
| *kapalı hat sessizdir* (§ Sessizlik) | **kural hiç eşleşmiyor** | cevap yok |
| *kapalı yetenek sessizdir* (burada) | kural eşleşti, **hat kapalı** | cevap yok |

İkisi de aynı felsefeden gelir: **kullanıcı istemediyse hiçbir şey olmaz.**
Fark şudur — birincisi kuralın kendisini, ikincisi kuralın *içindeki* bir
yeteceği susturur. `send_seen` gibi `*bool` alanlar bu kuralın dışında:
onlar susturmak değil, global ayarı geçersiz kılmaktır (bkz. § 2.1).

> ⚠️ Bu iki kural arasındaki fark, `NOTES.md`'de "sessizlik kasıtlıdır"
> maddesiyle birlikte okunmalı. Orada reddedilen şey, *sessizce kaybolmak
> yerine bir şey söylemek* isteğiydi; buradaki kural da aynı isteğin
> bayrak düzeyindeki hâlidir.

### Config yükleme sırasında doğrulanan kurallar

Bu eylem yazım hataları **config okunurken** yakalanır; motor
başlamadan önce hata verir. Böylece "yarım çalışan kural" olmaz:

| kural (kontrol sırasıyla) | hata |
|---|---|
| `session` yazılmış olmalı | `session required (opencode her yazılı mesajı bu oturuma verir)` |
| en az bir `allow_*` açık | `at least one allow_* must be true (nothing to do otherwise)` |
| `allow_help/cmd/durum/camera` açıksa `cmd:` şart | `cmd required (… need a script)` |
| `allow_gorsel/allow_belge` açıksa `gorsel:` şart | `gorsel required (… need a script)` |
| `allow_ses` açıksa `ses:` şart | `ses required (allow_ses needs a script)` |

> Sıra önemlidir: **ilk** çıkan hata `session`'dır. `session` yazılmadan
> yazılan `allow_*` bayraklarını gördüyseniz hatanın nedeni ordan gelmez;
> önce `session` yazın.

> Bir kuralın `allow_*` bayrağını kapatmak **kasıtlı** olabilir (kapalı
> hat). Bu yüzden kural "en az biri açık" ile sınırlı; hangisinin
> kapatıldığı serbesttir. Sıfır bayrağın hepsi kapalı olduğunda ise
> eylemin hiçbir işi yoktur ve bu yazım hatasıdır.

### Argümanlar otomatik eklenir

| alan | çalıştırılan biçim |
|---|---|
| `cmd` | `<cmd> "$TRIGGER_TEXT" "$TRIGGER_JID" "$TRIGGER_NAME"` |
| `gorsel` | `<gorsel> "$TRIGGER_ID"` |
| `ses` | `<ses> "$TRIGGER_ID"` |

Betiğin **argüman listesini bilmesi gerekmez** — hata riski tamamen
kalkar. (Eski hatlarda bu listeyi elle yazmak zorundaydı; birinde
`{mesajlar}` ile kimlik karıştırılıp 404 alınmıştı.)

### Eylem bazlı `*bool` geçersiz kılmalar

| alan | global karşılığı | davranış |
|---|---|---|
| `send_seen` | `okundu_isaretle` | mavi tik |
| `add_aitag` | `ai_eti_ekle` | "🤖 AI asistanı" etiketi |
| `do_debug` | `debug_cikti` | ajan ara mesajlarını gönder |

Bunlar **`*bool`**: üç durumları vardır —

- **yazılmamış** → global ayar geçerli
- **`true`/`false`** → o eylem için globali **geçersiz kılar**

Neden `*bool` ve sade `bool` değil: `send_seen: false` yazmakla hiç
yazmamak **aynı şey olsaydı**, bir eylem için mavi tik'i kapatmak
imkânsız olurdu (global açıkken). Ayrım bu yüzden zorunlu.

> `do_debug: true` ajanın **ara mesajlarını** (düşünce ve tool çıktısı
> hariç) ayrı ayrı gönderir. `false` ise yalnız **son `text` parçasını**
> gönderir. Tool çıktısı ve düşünme her zaman elenir — kullanıcıya
> gitmez.

### `--format json`: neden yalnız son cevap gidiyor

`opencode run` **hep** `--format json` ile çağrılır (config'e yazılır
değildir, tetikleyici ekler). Prompt'tan ÖNCE gelmek zorundur — opencode
mesajı sondan okur.

JSON'ı açmak çıktıdaki iki şeyi ayırır: **metin** parçaları ve **tool
çıktısı**. Olası olaylar adım adım gelir:

```
step_start → tool_use → step_finish → step_start → text → text
```

- **tool çıktısı** ve **düşünce** her koşulda elenir — hiç gönderilmez.
- **metin** parçaları sırayla toplanır; **en sonuncusu** nihai cevaptır.
  Önce gelenler ajanın ara konuşmalarıdır ("bir saniye", "dosyayı
  okuyorum").
- `do_debug: false` ise yalnız son metin gönderilir, aralardaki günlüğe
  düşer.

> **JSON neden şart?** Alınmazsa stdout, ajanın metni ile tool çıktısının
> **karışımı** olur — tek bir düz metin bloğu. O banttan yalnız ajanın
> cümlesini ayıklamak mümkün değildir, karışıklık doğrudan kullanıcıya
> gider. Olayları (`type: "text"` alanına bakarak) tek tek ayıklayıp tool
> çıktısını ve düşünmeyi atmak ancak JSON ile güvenilir yapılır.
>
> ⚠️ **JSON çözülemezse ham çıktı aynen döner.** Sessizce "boş cevap"
> vermek yerine (eski opencode sürümü ya da farklı bir çıktı biçimi) eski
> davranış korunur — ama bunun anlamı, ayrıştırılamayan hattan gelenin
> kullanıcıya gidebileceğidir.

---

## 2.2 `notify` — tetikleyicinin kendi olay bildirimleri

Kural DEĞİLDİR: mesaj eşleşmesiyle ilgisi yoktur. Tetikleyici açılınca,
kapanınca, köprü durumu değişince ve config dosyası değişince yalnız
`to` listesine kısa bir metin gider — "tetikleyici ayakta mı" sorusunun
cevabı sahibin cebine düşer.

```yaml
notify:
  to: ["905551234567", "12345678901234@lid"]
  on_start: true          # tetikleyici açıldı
  on_stop: true           # tetikleyici kapandı (SIGTERM)
  on_bridge_up: true      # köprü yeniden ulaşılır oldu
  on_bridge_down: true    # köprüye ulaşılamıyor
  on_config_change: true  # config dosyası değişti
  throttle: 5m
  retry: 30s
  max_retry: 10
```

### Ne zaman ne gider

| olay | metin | ne zaman |
|---|---|---|
| `on_start` | ✅ Tetikleyici açıldı | süreç başında, bir kez |
| `on_stop` | 🚦 Tetikleyici durduruldu | kapanışta (SIGTERM), son şans |
| `on_bridge_up` | 🔌 Köprü bağlandı | köprü sağlığı: down → up GEÇİŞİ |
| `on_bridge_down` | ⛔ Köprüye ulaşılamıyor | up → down GEÇİŞİ |
| `on_config_change` | ⚙️ Config değişti | dosya damgası (boyut+mtime) değişti |

### Spam koruması — 4 kural

1. **Aynı tür, `throttle` içinde bir daha gitmez.** Damgalar DOSYADA
   tutulur (`.tetikleyici/bildirim.txt`) — tetikleyici yeniden başlasa da
   hatırlar. Yoksa her restart'ta `on_start` bir daha giderdi.
   **Canlı ölçüldü (2026-10-06):** restart sonrası ikinci `on_start`
   gitmedi.
2. **Köprü bağlı değilse `max_retry` kez, `retry` aralığıyla denenir**,
   sonra günlüğe yazılıp vazgeçilir. Gönderilemeyeceği belli, ısrar
   anlamsız.
3. **Yalnız `to`'daki kimliklere gider.** ⛔ KENDİ NUMARANI (köprü
   hesabının numarası) buraya yazma. Tetikleyici köprünün numarasını
   bilemez (health'ta yok), koruma config seviyesindedir.
   ⛔ **AYNI KİŞİ İÇİN HEM TELEFON HEM LID YAZMA** → her bildirim 2
   kanaldan gider, alıcı her mesajı 2 kez görür. **Canlı ölçüldü
   (2026-10-06):** `["905551234567", "12345678901234@lid"]` yazılıydı,
   sahibin şikâyeti "bana 2'şer kere mesaj geldi" oldu; köprü kaydında
   aynı zaman damgalı 2'şer kayıt vardı. Tek kimlik kullan (tercihen LID).
4. **Başarısız gönderim damgalanmaz** — sonraki olay yeniden deneyebilir.
   Hata mesajı sahibe GİTMEZ (köprü düştü, cevap gönderecek hâlde
   değiliz), yalnız günlüğe yazılır.

### ⚙️ Notify ayarları her olayda DOSYADAN taze okunur

Config "mesaj başına yeniden okunur"; `engine.snapshot()` da yalnız MESAJ
geldiğinde yenilenir. Bildirim olayları mesaj akışından BAĞIMSIZ çalıştığı
için bu snapshot bayat kalabilir — config'i düzenleyip watcher'ın olay
üretmesi ile yeni ayarın görülmesi arasında boşluk vardı. **Canlı ölçülen
tuzak:** `to` tek LID'e düşürüldükten sonra çalışan süreç bir sonraki
config bildirimini ESKİ çift hedefle gönderdi (çift kanala gitti).
`guncelNotify` bu yüzden her olayda dosyayı doğrudan taze okur; okunamazsa
son snapshot'a düşer (bozuk düzenleme bildirimleri götürmez).

### ⛔ Akış kopması ≠ köprü düşüşü

Köprü SSE'yi **her ~30 saniyede bilerek kapatır**. Akış kopması rutindir;
`on_bridge_down` bundan TETİKLENMEZ. Gerçek ölçüt `/health`'tir ve yalnız
**durum değişiminde** (up→down ya da down→up) bildirilir. İlk gözlem
sessizdir: tetikleyici başlarken köprü zaten düşükse "düştü" diye
bağırmayız.

### Neden `on_stop` tek denemedir

Kapanışta süreç çıkıyor; `on_stop`'un bağlamı 25 saniye ile sınırlıdır
(`durdur` dönünce tetikleyici kapanır). Köprü ayaktaysa tek deneme yeter;
düşükse bütçe tükenip vazgeçilir. `retry`/`max_retry` asıl `on_bridge_*`
ve `on_start` için anlamlıdır.

---

## 2.3 `tehlikeli_mesaj` — tanınmayan kaynaklardan şüpheli mesaj uyarısı

Kural DEĞİLDİR ve **YANIT DEĞİLDİR**: gelen numaraya hiçbir şey
gönderilmez. Kurallarda **tanınmayan** bir numaradan link içeren metin ya
da dosya/medya eki gelirse, kısa bir uyarı yalnız `to`'daki kimliğe gider
— yabancı bir hesabın "şüpheli" davranışından admin'in haberi olur.

```yaml
tehlikeli_mesaj:
  acik: false                 # varsayılan KAPALI (MEVZU)
  to: "12345678901234@lid"    # uyarının gideceği kimlik (mefamex)
  aralik: "1h"                # aynı numaradan uyarılar arası asgari süre
```

### Ne şüpheli sayılır (şimdilik)

| sinyal | örnek |
|---|---|
| metinde URL/link | `http://` · `https://` · `www.` |
| dosya/medya eki | `type: image` `video` `audio` `document` |

### ⛔ "Kapalı hat sessizdir" (2026-10-06 KALICI KURAL) nasıl korunur

1. **Kimseye cevap verilmez** — uyarı admin'e gider, mesajı gönderene değil.
2. **Kurallarda tanınan kimlikler uyarıya girmez.** `number` (From/Sender)
   ya da `name` (PushName) eşleşmesi olan herkes "tanınan" sayılır.
   Burak/Yusuf/Masha kuralları kapalı olsa bile onlardan gelen NORMAL
   mesajlar admin'e "şüpheli" diye düşmez.
3. **Yalnız eşleşmeyen mesajlar taranır** — normal akışı hiç etkilemez.
4. **Aynı numara `aralik` içinde bir daha uyarılmaz** (spam koruması;
   varsayılan 1 saat). Sayaç bellekte tutulur, restart'ta sıfırlanır —
   uyarı yanıt olmadığı için bu zararsızdır.

### Neden "sahte pozitif zararsız, sahte negatif sessiz"

Uyarı yalnız admin'e gider; yanlış bir uyarı kimseyi yanlış cevaplamaz.
Kaçırılan bir sinyal ise sessizdir — özellik kapatılabilir, hat hiçbir
zaman bir kullanıcıya otomatik mesaj atamaz. Kimlik avı linkleri ve bulaşık
dosyalar kötü niyetli trafiğin en sık taşıdığı iki yüktür; ilk sürüm onları
tarar.

---

## 3. Tüm opsiyonlar — tek örnek

```yaml
  - name: ornek-kural
    match:
      everyone: false              # koşulsuz her mesaj
      number: ["905551234567", "12345678901234@lid"]
      name: [Mefamex, Yusuf]       # öncelikli OR
      text: müfredat               # AND — metin içermeli
      not_text: ping               # AND — "ping" varsa kural TUTMAZ
      type: [audio, image]         # AND — mesaj tipi
      group: false                 # gruplarda çalışma
    do:
      - type: command              # 1) kabuk komutu / script
        code: ~/bin/kaydet.sh "$TRIGGER_ID"
        reply: "kaydedildi"
        timeout: 30s
      - type: session              # 2) sabit opencode oturumu
        session: ses_XXX
        reply: true
        agent: wp-sahip
        timeout: 12m
      - type: new_session          # 3) her seferinde yeni oturum
        reply: true
```

---

## 4. Örnekler

```yaml
# Her mesajı bir komuta bağla
match: { everyone: true }
do:    [ { type: command, code: "echo 'her mesaj' >> /tmp/rapor.txt" } ]

# Sadece Mefamex'e script çalıştır
match: { number: ["905551234567", "12345678901234@lid"], name: Mefamex }
do:    [ { type: command, code: "~/bin/islem.sh" } ]

# Mefamex 'kitap' derse ajana sor
match: { name: Mefamex, text: kitap }
do:    [ { type: session, session: ses_XXX, reply: true } ]

# Sesli notu oku  (dikkat: $TRIGGER_ID, {mesajlar} değil)
match: { name: Mefamex, type: audio }
do:    [ { type: command, code: "~/bin/ses.py \"$TRIGGER_ID\"", reply: true } ]

# Birden çok kişi
match: { number: [905550000002, 905550000003], name: [Burak, Yusuf] }
do:    [ { type: command, code: "~/bin/toplu.sh" } ]

# Yalnız gruplarda, gruplardaki Yusuf'a
match: { group: true, name: Yusuf }
do:    [ { type: command, code: "~/bin/grup.sh" } ]

# Her mesaja sabit cevap
match: { everyone: true }
do:    [ { type: command, code: "true", reply: "buradayım" } ]

# Bir kuralda iki iş sırayla
do:
  - { type: command, code: "echo >> /tmp/arsiv.log" }
  - { type: session, session: ses_XXX, reply: true }
```

---

## 5. ⚠️ Tuzaklar

### `{mesajlar}` mesaj **metnidir**, kimlik **değildir**

Bu karışıklık **canlıda oldu**: sesli not kuralında `{mesajlar}` yazılıydı,
script metni kimlik sanıp `/media/<metin>` çağırdı, **404** aldı, eylem
başarısız oldu ve **sessizce** cevap gitmedi.

| Değişken | Ne verir |
|---|---|
| `{mesajlar}` | mesaj **metni** (son mesajların okunabilir hâli) |
| `$TRIGGER_ID` | mesaj **kimliği** ← `/media/{id}` ucu bunu ister |
| `$TRIGGER_TEXT` | tetikleyen mesajın metni |

> `code:` bir **dosya yoluna** işaret ediyorsa `{mesajlar}` dosyanın
> *içinde* genişletilmez — `TRIGGER_*` değişkenlerini kullan.
>
> Kimlik tırnaklı yazılır: `"$TRIGGER_ID"`. Tırnaksız hâlde `sh -c`
> içinde özel karakterlerde bozulur.

### Komutun beslendiği kanallar

```
stdin .............. ham mesaj metni
TRIGGER_TEXT ....... tetikleyen mesajın metni
TRIGGER_MESSAGES ... son mesajlar, "isim: metin" biçiminde
TRIGGER_NAME ....... gönderenin push_name
TRIGGER_JID ........ sohbet kimliği
TRIGGER_SENDER ..... gönderenin ham kimliği
TRIGGER_ID / TRIGGER_TYPE / TRIGGER_TIME
TRIGGER_JSON ....... tam veri JSON
TRIGGER_CONFIG ..... bu config dosyasının yolu
```

### Çift gönderim

`session`/`new_session` eyleminde ajan, tetikleyen sohbete **kendisi**
`whatsapp_send` ile yazmamalı — çıktısı zaten cevap olarak gider.
İkisini de yaparsa karşı taraf her şeyi **iki kez** alır (canlıda oldu:
Masha'ya cevap + "Yanıt gönderildi" raporu peş peşe).

Artık güvence **prompt'ta değil, kodda**: tekrar eden metin
`action.go → gonder()` katmanında bastırılır (2 dk pencere).
Ajanın `whatsapp_send` izni **açık kalmalıdır** — kapatmak ajanı körleştirir.

### PATH

Servis minimal PATH ile çalışır. Script içinde binary'leri **mutlak
yolla** çağır (`~/.opencode/bin/opencode`), yoksa `exit 127`.

### Aynı mesaj iki kez çalışmaz

`.tetikleyici/seen.txt` ile tutulur.

### Bir mesaja kaç kural tutarsa HEPSİ çalışır

İlki değil — sırayla hepsi. Bu yüzden `mefamex-ping` ile
`mefamex-oturum`'un `not_text: ping` ile birbirini dışlaması gerekir.

### Config bozulursa

Son **GEÇERLİ** kurallar korunur, araç ayakta kalır, sadece hata
loglanır.

---

## 6. Pasif kuralları geri açma

Config'in altında yorumda, hazır bekleyen 6 kural var (Burak, Yusuf,
Masha). **Bunların karşılığı cevap değil hiçbir şeydir** — bkz. en üstteki
⛔ bölümü.

Yerleşim tercihi: **kurallar en üstte, açıklamalar altta.** Bu yüzden
pasif kurallar `rules:` listesinin dışında, dosyanın en sonunda duruyor —
`rules:` yalnız aktif kuralları okur.

### Adımlar

**Tek işaretçi: `#   - name: burak-ping`.** O satırdan **dosya sonuna kadar**
her şeyi kopyala. (Blokun en üstündeki 6 dekoratif satır yorumda kalır —
onları alma, orası kural değil.)

1. `#   - name: burak-ping` satırından dosya sonuna kadar kopyala.
2. Her satırın başındaki `# ` sil — 6 kuralın her birinde 7 satır var:
   `#   - name: burak-ping` → `  - name: burak-ping`  (3 boşluk → 2)
   `#     timeout: 12m`     → `    timeout: 12m`      (5 → 4)
   Satırın geri kalanı **aynen** kalır. İç yorumlar (`#   # ── MİSAFİR…`)
   yorum olarak kalır, YAML'ı bozmaz.
3. Yapıştırma yeri = **`rules:` listesinin sonu** — `mefamex-gorsel`
   kuralının `timeout: 12m` satırından sonra.
4. Dosyanın altında artık boş kalan `⛔` ve `⏸` başlık yorumlarını sil
   (kurallar artık `rules:` içinde, orada bir şey kalmadı).
5. **Doğrula** — 10 demeli:

   ```bash
   python3 -c "import yaml;print(len(yaml.safe_load(open('tetikleyici.yaml'))['rules']))"
   ```

6. Restart gerekmez (her mesajda okunur). Yine de tetikleyici açıksa
   `./tetikleyici.sh restart` ile log'da "10 kural" gör.

> Bu adımlar 2026-10-06'da gerçekten çalıştırılarak denendi: **4 → 10 kural**,
> oturum kimlikleri (`ses_ef7c…`) ve `wp-misafir` ajanı bozulmadan geri geldi.

Numaralar, oturum kimlikleri (`ses_ef7c…`) ve ajan adları (`wp-misafir`)
bloğun içinde aynen duruyor. `opencode.json`'daki ajan tanımlarına
dokunmak gerekmez.

### Bu işten sonra

Geri açmak, kuralları `rules:` listesine taşımaktır — **cevap kodu
yazılmaz.** Sessizlik kuralı (en üstteki ⛔ bölümü) bundan sonra da
geçerlidir; kural açık olmak, karşısına otomatik cevap koymak
demek değildir.
---

## 🔐 Config'e yazılan özellik ATILIR MI? — 3 aşamalı yetki

> Sahibi 2026-10-06'da sordu: *"config içine yazılan … atılacak mı mevzusu"*
> ve üç aşama verdi. Kural burada da geçerlidir.

| Aşama | Nerede | Anlamı |
|---|---|---|
| **1** | **Kod varsayılanı = `false`** | Hiçbir yerde yazılmamışsa özellik **kapalıdır** |
| **2** | **Config başındaki beyan** | Açıkça yazılırsa uygulanır |
| **3** | **Kullanıcı ayarı** | **Config başını ezer** — en yüksek öncelik |

```
kullanıcı ayarı   ← en yüksek (config'i ezer)
config başı      ← orta
kod varsayılanı  ← false, en düşük (temel güvenlik)
```

### Cevap: "atılacak mı?"

| Config'de | Kullanıcı ayarı | Sonuç |
|---|---|---|
| yazılmamış | — | ❌ **ATILIR** — özellik çalışmaz |
| `true` | — | ✅ uygulanır |
| `true` | `false` | ❌ **ATILIR** — kullanıcı ayarı ezdi |

> **Neden kod `false`:** yeni bir özellik eklenirken `true` unutulursa
> güvenli tarafta kalır. "Config'i unutma" hatası böyle kapanır.
>
> Tam gerekçe: `TODO.md` → *"MEVZU: config'e yazılan şey ATILACAK MI?"*

### Yeni boolean eklerken

1. Kodda **`false`** ile tanımla (varsayılan açık olmaz).
2. `tetikleyici.yaml` **başına** beyan satırı ekle.
3. `config.readme.md`'ye yaz (bu tablo).
4. Üç senaryoyu test et: yalnız kod · kod+config · kod+config+kullanıcı.
