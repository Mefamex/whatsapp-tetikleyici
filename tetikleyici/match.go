// whatsapp-bridge-project/tetikleyici/match.go

package main

import (
	"strings"
)

// matches, mesajın koşulun tamamını sağlayıp sağlamadığını bildirir.
//
// Kimlik (numara/isim) **öncelikli OR**, içerik (metin/grup) **AND** ile
// değerlendirilir. Ayrıntı için resolve ve contentHolds.
func (c condition) matches(m message) bool {
	if m.fromMe() {
		// Ürettiğimiz bir cevap, onu üreten akışı yeniden tetiklememeli.
		return false
	}
	return c.resolve(m).matched && c.contentHolds(m)
}

// identity, kimlik koşulunun hangi yolla sağlandığını bildirir.
type identity struct {
	// matched, kimlik koşulunun tuttuğunu söyler.
	matched bool

	// via tutturucu alanın adıdır: "number", "name" ya da "everyone".
	via string

	// detail eşleşen değerdir; günlükte "hangi numara/isim tuttu" bilgisidir.
	detail string
}

// resolve, mesajın kimliğini öncelikli sırayla çözer.
//
// Önce numara denenir. LID'li sohbetlerde numara hiçbir alanda bulunmadığı
// için deneme tutmaz; o zaman isme bakılır. Böylece kullanıcı tek kuralda
// ikisini de yazarak sohbetin hangi biçimde açılırsa açılsın yakalayabilir.
//
// Hiçbir kimlik alanı yazılmamışsa kısıt yoktur.
func (c condition) resolve(m message) identity {
	if len(c.Numbers) > 0 {
		if want, hit := firstHit(c.Numbers, m.From, m.Sender); hit {
			return identity{matched: true, via: "number", detail: want}
		}
	}

	if len(c.Names) > 0 {
		if want, hit := firstHit(c.Names, m.PushName); hit {
			return identity{matched: true, via: "name", detail: want}
		}
	}

	if len(c.Numbers) == 0 && len(c.Names) == 0 {
		return identity{matched: true, via: "everyone"}
	}

	return identity{matched: false}
}

// contentHolds, içerik koşullarını değerlendirir: metin, tip, grup ve önek birlikte
// AND'lanır. Kimlik tutsa bile metin tutmuyorsa kural tutmaz.
func (c condition) contentHolds(m message) bool {
	if c.GroupsOnly && !m.IsGroup {
		return false
	}
	// Metni olmayan mesaj, metin bekleyen hiçbir kuralı tutmaz.
	if c.metinBekliyor() && strings.TrimSpace(m.Text) == "" {
		return false
	}
	if c.Text != "" && !containsFold(m.Text, c.Text) {
		return false
	}
	if c.NotText != "" && containsFold(m.Text, c.NotText) {
		return false
	}
	if c.Prefix != "" && !hasPrefixFold(m.Text, c.Prefix) {
		return false
	}
	if c.NotPrefix != "" && hasPrefixFold(m.Text, c.NotPrefix) {
		return false
	}
	if len(c.Types) > 0 {
		if _, hit := firstHit(c.Types, m.Type); !hit {
			return false
		}
	}
	return true
}

// Metin bekleyen kural, metni olmayan mesajı TUTMAZ.
//
// Sticker, reaksiyon ve bazı boş payload'larda metin yoktur. `type` kısıtı
// olmayan bir kural bunları da yakalayıp ajanı boş pencereyle tetikliyordu;
// ajan da tetikleyen mesajı değil penceredeki başka bir mesajı işliyordu.
// Ölçüm: export/1-mefamex — Mefamex "durum" yazdı, ajan "Sesli mesajı aldım,
// 'durum' dedin" diye cevap verdi.
//
// Kural metin bekliyorsa (text/not_text/prefix) boş metin elenir. `everyone` + komut
// gibi "metin aramayan" kurallar etkilenmez.
//
// DİKKAT: `type` kısıtı "metin bekliyor" SAYILMAZ. `type: audio` kuralı tamamen
// metinsiz bir sesli notu yakalamak için vardır; bu koruma onu elersdi sesli
// not hattı çalışmazdı (ilk yazımda 4 test kırıldı, düzeltildi). Tip koşulu
// zaten kendi satırında eleniyor.
func (c condition) metinBekliyor() bool {
	return c.Text != "" || c.NotText != "" || c.Prefix != "" || c.NotPrefix != ""
}

// detailLine, kimliğin nasıl sağlandığını günlük için okunabilir biçimde döner.
func (i identity) detailLine() string {
	switch i.via {
	case "number":
		return "number=" + i.detail
	case "name":
		// LID chats don't have number in payload, so falling back to name
		// is normal; log this explicitly so user understands the fallback.
		return "name=" + i.detail + " (number not in payload)"
	default:
		return "everyone"
	}
}

// describe, koşülü tek satırda gösterir — status çıktısı ve günlük için.
// Öncelik sırası `number → name` olarak gösterilir ki kullanıcı yazdığı
// düzeni tanıyabilsin.
func (c condition) describe() string {
	if c.isEmpty() {
		return "everyone"
	}

	var parts []string
	if len(c.Numbers) > 0 {
		parts = append(parts, "number="+strings.Join(c.Numbers, "|"))
	}
	if len(c.Names) > 0 {
		suffix := "name=" + strings.Join(c.Names, "|")
		if len(c.Numbers) > 0 {
			// If both present, name is the fallback.
			suffix += " (fallback)"
		}
		parts = append(parts, suffix)
	}
	if c.Text != "" {
		parts = append(parts, "text="+truncate(c.Text, 30))
	}
	if c.NotText != "" {
		parts = append(parts, "not_text="+truncate(c.NotText, 30))
	}
	if c.Prefix != "" {
		parts = append(parts, "prefix="+truncate(c.Prefix, 30))
	}
	if c.NotPrefix != "" {
		parts = append(parts, "not_prefix="+truncate(c.NotPrefix, 30))
	}
	if len(c.Types) > 0 {
		parts = append(parts, "type="+strings.Join(c.Types, "|"))
	}
	if c.GroupsOnly {
		parts = append(parts, "group")
	}

	return strings.Join(parts, " ")
}

// isEmpty, koşülün hiçbir kısıt getirip getirmediğini bildirir.
func (c condition) isEmpty() bool {
	return !c.GroupsOnly && c.Text == "" && c.NotText == "" && len(c.Types) == 0 &&
		len(c.Numbers) == 0 && len(c.Names) == 0
}

// firstHit, istenen değerlerden hangisinin alanda geçtiğini bildirir.
func firstHit(wants []string, fields ...string) (string, bool) {
	for _, want := range wants {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		for _, field := range fields {
			if containsFold(field, want) {
				return want, true
			}
		}
	}
	return "", false
}

// containsFold, büyük/küçük harf duyarsız alt dizi aramasıdır.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// hasPrefixFold, büyük/küçük harf duyarsız önek kontrolüdür.
func hasPrefixFold(haystack, needle string) bool {
	return strings.HasPrefix(strings.ToLower(haystack), strings.ToLower(needle))
}

// matchingRules, koşulu tutan tüm kuralları config sırasıyla döndürür.
//
// Yalnız ilki değil hepsi: "herkese kaydet" ile "Mefamex'e cevap ver" kuralları
// aynı mesajda birlikte çalışabilmelidir. Sıra config'in işidir.
func matchingRules(set ruleSet, m message) []rule {
	var hits []rule
	for _, r := range set.Rules {
		if r.Match.matches(m) {
			hits = append(hits, r)
		}
	}
	return hits
}

// seedJIDs, canlı trafik görülmeden önce taranacak sohbetleri verir.
//
// Yalnız config'de **açıkça yazılmış** sohbetler taranır. Çıplak rakamdan yalnız
// telefon biçimi üretilir; LID biçimi türetilemez, çünkü LID tamamen farklı bir
// kimlik uzayıdır (telefon 905551234567 → LID 12345678901234 gibi). Uydurma bir
// LID taramak hiçbir işe yaramaz ve kullanıcıya kapsama varmış izlenimi verir.
//
// LID kullanan sohbetler config'te tam kimlikle yazılmalı, ya da canlı trafikle
// öğrenilmelidir. İkisi de mümkün değilse kopuk pencerede o sohbete düşen mesaj
// kaçırılır; bu, LID'in doğası gereği bir sınırdır.
func seedJIDs(set ruleSet) []string {
	var out []string
	seen := make(map[string]bool)

	for _, r := range set.Rules {
		for _, number := range r.Match.Numbers {
			for _, jid := range jidForms(number) {
				if !seen[jid] {
					seen[jid] = true
					out = append(out, jid)
				}
			}
		}
	}
	return out
}

// jidForms, bir numara girdisinden taranacak sohbet kimliğini üretir.
func jidForms(number string) []string {
	number = strings.TrimSpace(number)
	if number == "" {
		return nil
	}
	if strings.Contains(number, "@") {
		return []string{number}
	}
	return []string{number + "@s.whatsapp.net"}
}

// needsExplicitLID, kuralların isimle sınırlı olup olmadığını bildirir.
//
// Böyle bir kural LID'li sohbetleri yakalayabilir ama o sohbetler seed
// listesinde olamaz; kullanıcıya başlangıçta bunu söylemek gerekir, yoksa
// "neden tetiklenmiyor?" diye arar.
func needsExplicitLID(set ruleSet) bool {
	for _, r := range set.Rules {
		if len(r.Match.Names) > 0 {
			return true
		}
	}
	return false
}
