# Optional wrapper: every target delegates to scripts/install.sh, which is the
# real entry point and works without make. Requires Go >= 1.27.
# GO and PREFIX are passed through to the script via the environment.
SCRIPT = scripts/install.sh

.PHONY: build test check-go install install-skill uninstall clean

build test check-go install install-skill uninstall clean:
	@$(SCRIPT) $@
