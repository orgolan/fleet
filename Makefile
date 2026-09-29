# fleet build/install. Requires Go >= 1.27 (see check-go).
# If your Go lives elsewhere: PATH=$$HOME/.local/go/bin:$$PATH make build
GO     ?= go
PREFIX ?= $(HOME)/.local
BIN     = bin/fleet
SKILL_SRC = $(CURDIR)/skills/fleet
SKILL_DST = $(HOME)/.claude/skills/fleet

.PHONY: build test check-go install install-skill uninstall clean

check-go:
	@v=$$($(GO) env GOVERSION 2>/dev/null | sed 's/^go//'); \
	if [ -z "$$v" ]; then echo "error: '$(GO)' not found; install Go >= 1.27 or set GO=/path/to/go (e.g. PATH=$$HOME/.local/go/bin:$$PATH)" >&2; exit 1; fi; \
	if [ "$$(printf '%s\n1.27\n' "$$v" | sort -V | head -n1)" != "1.27" ]; then \
	  echo "error: Go >= 1.27 required, found $$v (try PATH=$$HOME/.local/go/bin:$$PATH)" >&2; exit 1; fi

build: check-go
	$(GO) build -o $(BIN) ./cmd/fleet

test: check-go
	$(GO) vet ./...
	$(GO) test -race ./...

install: build install-skill
	install -d $(PREFIX)/bin
	install -m 0755 $(BIN) $(PREFIX)/bin/fleet
	@echo "installed $(PREFIX)/bin/fleet (make sure it is on PATH)"

install-skill:
	@mkdir -p "$(dir $(SKILL_DST))"
	@if [ -L "$(SKILL_DST)" ]; then rm "$(SKILL_DST)"; \
	elif [ -e "$(SKILL_DST)" ]; then echo "error: $(SKILL_DST) exists and is not a symlink; refusing to overwrite" >&2; exit 1; fi
	ln -s "$(SKILL_SRC)" "$(SKILL_DST)"
	@echo "linked $(SKILL_DST) -> $(SKILL_SRC)"

uninstall:
	rm -f $(PREFIX)/bin/fleet
	@if [ -L "$(SKILL_DST)" ]; then rm "$(SKILL_DST)"; echo "removed skill link"; \
	elif [ -e "$(SKILL_DST)" ]; then echo "leaving $(SKILL_DST): not a symlink" >&2; fi

clean:
	rm -rf bin
