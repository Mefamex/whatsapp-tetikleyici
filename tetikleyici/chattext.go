// whatsapp-bridge-project/tetikleyici/chattext.go

package main

import (
	"regexp"
	"strings"
)

// WhatsApp'ın kendi biçimlendirmesi Markdown'ın bir alt kümesidir:
//
//	*kalın*  _italik_  ~üstü çizili~  ```satır içi kod```
//
// Bir dil modeli Markdown üretir ve Markdown'ın **iki** yıldızla yazılan
// kalın vurgusunu WhatsApp anlamaz — mesajda `**metin**` yazısı görünür.
// Dönüşüm burada yapılır: model her zaman Markdown'a kaçsa bile giden mesaj
// düzgün görünür. Ters yönde bir işlem yapmıyoruz; bir komutun ham çıktısı
// (log, JSON) `code:` üzerinden geldiğinde biçim bozulmamalı, o yüzden
// yalnız model/yapay-zekâ yanıtları ile eşleşen örüntüler değiştirilir.
var chatPatterns = []struct {
	re   *regexp.Regexp
	to   string
	name string
}{
	// ```kod``` → ```kod```  (WhatsApp da üç backtick kullanır, dokunulmaz)
	{regexp.MustCompile(`\*\*\*(.+?)\*\*\*`), `*` + "$1" + `*`, "kalın-italik"},
	{regexp.MustCompile(`\*\*(.+?)\*\*`), `*` + "$1" + `*`, "kalın"},
	{regexp.MustCompile(`(?m)^\s*#{1,6}\s+`), "", "başlık işareti"},
	{regexp.MustCompile(`(?m)^\s*[-*+]\s+`), "• ", "maddeler"},
	{regexp.MustCompile(`\*\*(.+?)\*\*`), "*$1*", "kalın-kalın"},
	{regexp.MustCompile("`([^`\n]+)`"), "$1", "satır içi kod"},
}

// chatText, bir yanıtı WhatsApp'ın anladığı biçime çevirir.
//
// Amaç güzellik değil, doğru görünüm: model çıktısındaki **kalın** ve
// `kod` işaretleri mesajda olduğu gibi kalırsa kullanıcı ham markdown görür.
// Metnin kendisine dokunulmaz; yalnız işaretler çevrilir.
func chatText(s string) string {
	out := s

	for _, p := range chatPatterns {
		out = p.re.ReplaceAllString(out, p.to)
	}

	// Başlık işareti ve madde işaretleri satır başına uygulanır; işaret
	// silindikten sonra o satırda fazladan boşluk kalmış olabilir.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	out = strings.Join(lines, "\n")

	// Ardışık boş satırları teke indir: model çıktısında \n\n\n birikmesi
	// WhatsApp'ta gereksiz uzun kaydırma yaratır.
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(out)
}
