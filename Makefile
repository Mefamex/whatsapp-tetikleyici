# =============================================================================
# WhatsApp Tetikleyici (Event Dispatcher) — Makefile
#
# Kendi kendini belgeleyen (Self-documenting) görev yöneticisi.
# Terminalde sadece 'make' yazıldığında kullanılabilir komutları listeler.
#
# ZORUNLU YAŞAM DÖNGÜSÜ HEDEFLERİ:
# [install, reinstall, update, run, clean, remove]
# =============================================================================

.DEFAULT_GOAL := help
.PHONY: help install reinstall update run dev lint test verify build clean remove

help: ## Mevcut komutları ve açıklamalarını listeler
	@echo "Kullanım: make [komut]"
	@echo ""
	@echo "Kullanılabilir Komutlar:"
	@LC_ALL=C grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# --- 1. KURULUM VE YAŞAM DÖNGÜSÜ ---

install: ## Bağımlılıkları kurar (Go modülleri + Python uv sanal ortamı)
	@echo "==> Go bağımlılıkları indiriliyor..."
	cd tetikleyici && go mod download
	@echo "==> Python sanal ortamı ve gereksinimler kuruluyor..."
	uv venv .venv
	uv pip install -r requirements.txt
	@echo "==> Kurulum tamamlandı."

reinstall: remove install ## Kurulu ortamı kaldırıp sıfırdan yeniden kurar
	@echo "==> Temiz yeniden kurulum tamamlandı."

update: ## Bağımlılıkları en son sürümlere günceller
	@echo "==> Bağımlılıklar güncelleniyor..."
	cd tetikleyici && go get -u ./... && go mod tidy
	uv pip install --upgrade -r requirements.txt

remove: ## Bağımlılıkları, sanal ortamı ve derleme çıktılarını kazır
	@echo "==> Sanal ortam ve derleme çıktıları kaldırılıyor..."
	rm -rf .venv bin/ tetikleyici/tetikleyici
	@echo "Kaldırma işlemi tamamlandı."

# --- 2. ÇALIŞTIRMA VE GELİŞTİRME ---

build: ## Tetikleyici Go ikili dosyasını derler (bin/tetikleyici)
	@echo "==> Tetikleyici derleniyor..."
	mkdir -p bin
	cd tetikleyici && go build -o ../bin/tetikleyici .
	@echo "==> Derleme başarılı: bin/tetikleyici"

run: build ## Tetikleyici daemon'ını başlatır
	@echo "==> Tetikleyici başlatılıyor..."
	./bin/tetikleyici

dev: build ## Tetikleyiciyi canlı geliştirme modunda başlatır
	@echo "==> Geliştirme modu başlatılıyor..."
	./bin/tetikleyici

# --- 3. DENETİM VE DOĞRULAMA ---

lint: ## Go ve Python için statik kod analizi yapar
	@echo "==> Go vet analizi yapılıyor..."
	cd tetikleyici && go vet ./...
	@echo "==> Python sözdizimi denetleniyor..."
	.venv/bin/python -m py_compile *.py gorsel/*.py tetikleyici/cmd/*.py

test: ## Go ve Python birim testlerini çalıştırır
	@echo "==> Go birim testleri koşturuluyor..."
	cd tetikleyici && go test ./...
	@echo "==> Python boru hattı ve tünel testleri koşturuluyor..."
	PYTHONPATH=.:tetikleyici/cmd .venv/bin/python -m unittest discover -s tests -p 'test_*.py' -v

verify: lint test ## Commit/Push öncesi tam doğrulama zincirini çalıştırır (lint + test)
	@echo "==> TÜM DOĞRULAMALAR BAŞARIYLA GEÇTİ (ALL_OK)."

# --- 4. TEMİZLİK ---

clean: ## Geçici dosyaları, test önbelleklerini ve kalıntıları temizler
	@echo "==> Kalıntılar temizleniyor..."
	find . -type d -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
	rm -rf .pytest_cache .ruff_cache tests/__pycache__ gorsel/__pycache__ tetikleyici/cmd/__pycache__
	@echo "Temizlik tamamlandı."
