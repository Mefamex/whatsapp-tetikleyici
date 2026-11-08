package main

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// stringList, YAML'da tek değer ya da liste olarak yazılabilir.
//
// Kullanıcı tek kişi yazarken dizi içine almak zorunda kalmamak için iki biçim
// de kabul edilir. Virgülle ayrılmış tek değerler de ayrıştırılır, çünkü elle
// yazımda tırnak işareti unutmak kolaydır.
type stringList []string

// UnmarshalYAML, düz bir değeri veya listeyi kabul eder.
func (l *stringList) UnmarshalYAML(value *yaml.Node) error {
	// Hedef düğüme in: liste elemanları da aynı tipten olduğu için
	// otomatik olarak çalışmaz, elle ayrıştırmak gerekir.
	var resolved yaml.Node
	if err := value.Decode(&resolved); err != nil {
		return err
	}

	if resolved.Kind == yaml.ScalarNode {
		var single string
		if err := resolved.Decode(&single); err != nil {
			return err
		}
		*l = splitList(single)
		return nil
	}

	if resolved.Kind != yaml.SequenceNode {
		return &yaml.TypeError{Errors: []string{"değer ya da liste olmalı"}}
	}

	*l = nil
	for _, item := range resolved.Content {
		var text string
		if err := item.Decode(&text); err != nil {
			return err
		}
		*l = append(*l, splitList(text)...)
	}

	return nil
}

// splitList, virgülle ayrılmış bir değeri boş olmayan parçalara böler.
func splitList(value string) []string {
	parts := strings.Split(value, ",")

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
