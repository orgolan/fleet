# Optional wrapper: every target delegates to scripts/install.sh, which is the
# real entry point and works without make. Requires Go >= 1.27.
# GO and PREFIX are passed through to the script via the environment.
SCRIPT = scripts/install.sh

.PHONY: setup build test check-go install install-skill uninstall clean doctor release

setup build test check-go install install-skill uninstall clean:
	@$(SCRIPT) $@

release:
	@scripts/release.sh $(VERSION)

doctor:
	@scripts/doctor.sh
