# Packaging targets. The binary itself has no build dependencies beyond Go;
# everything here is about getting it onto a machine the way that machine
# expects software to arrive.

# -buildvcs=false throughout: the version is stamped through ldflags, and VCS
# stamping fails outright when the build runs in a container that does not own
# the checkout, which is exactly how the rpm is built.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION := $(patsubst v%,%,$(VERSION))
LDFLAGS := -s -w -X main.version=$(VERSION)
DIST    := dist
PLATFORMS := linux/amd64 linux/arm64

.PHONY: all build test dist deb rpm clean checksums

all: build

build:
	go build -ldflags '$(LDFLAGS)' -o reclaim ./cmd/reclaim

test:
	go test ./...
	go vet ./...

# Static binaries: this is a tool for a machine that is having a bad day, and
# a dynamic link to a libc that is also on the failing disk is a bad bet.
dist: clean
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "  $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o $(DIST)/reclaim ./cmd/reclaim || exit 1; \
		tar -C $(DIST) -czf $(DIST)/reclaim_$(VERSION)_$${os}_$${arch}.tar.gz reclaim; \
		rm -f $(DIST)/reclaim; \
	done
	@$(MAKE) --no-print-directory checksums

checksums:
	@cd $(DIST) && sha256sum *.tar.gz *.deb *.rpm 2>/dev/null > SHA256SUMS || true
	@cd $(DIST) && sha256sum -c SHA256SUMS >/dev/null 2>&1 && echo "  checksums ok" || true

# Built with dpkg-deb rather than a packaging framework: a .deb is a control
# file and a file tree, and adding a dependency to produce one would undo the
# point of shipping a binary with none.
deb:
	@command -v dpkg-deb >/dev/null || { echo "dpkg-deb not found"; exit 1; }
	@for arch in amd64 arm64; do \
		root=$(DIST)/deb-$$arch; \
		rm -rf $$root; \
		mkdir -p $$root/DEBIAN $$root/usr/bin $$root/usr/share/doc/reclaim \
			$$root/usr/share/bash-completion/completions \
			$$root/usr/share/zsh/site-functions \
			$$root/usr/share/fish/vendor_completions.d; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch \
			go build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o $$root/usr/bin/reclaim ./cmd/reclaim || exit 1; \
		sed -e 's/@VERSION@/$(VERSION)/' -e "s/@ARCH@/$$arch/" packaging/control.in > $$root/DEBIAN/control; \
		cp LICENSE README.md $$root/usr/share/doc/reclaim/; \
		$$root/usr/bin/reclaim completion bash > $$root/usr/share/bash-completion/completions/reclaim 2>/dev/null || \
			./reclaim completion bash > $$root/usr/share/bash-completion/completions/reclaim; \
		$$root/usr/bin/reclaim completion zsh > $$root/usr/share/zsh/site-functions/_reclaim 2>/dev/null || \
			./reclaim completion zsh > $$root/usr/share/zsh/site-functions/_reclaim; \
		$$root/usr/bin/reclaim completion fish > $$root/usr/share/fish/vendor_completions.d/reclaim.fish 2>/dev/null || \
			./reclaim completion fish > $$root/usr/share/fish/vendor_completions.d/reclaim.fish; \
		dpkg-deb --build --root-owner-group $$root $(DIST)/reclaim_$(VERSION)_$$arch.deb >/dev/null; \
		rm -rf $$root; \
		echo "  $(DIST)/reclaim_$(VERSION)_$$arch.deb"; \
	done

rpm:
	@command -v rpmbuild >/dev/null || { echo "rpmbuild not found"; exit 1; }
	@mkdir -p $(DIST) $$HOME/rpmbuild/SOURCES
	@CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o $(DIST)/reclaim ./cmd/reclaim
	@tar -C $(DIST) -czf $$HOME/rpmbuild/SOURCES/reclaim-$(VERSION).tar.gz reclaim
	@# dist %{nil}: without it the name carries the build host's tag
	@# (reclaim-0.2.0-1.fc44.x86_64.rpm), which is both wrong for a static
	@# binary that runs anywhere and a name no installer can predict.
	@rpmbuild -bb --define "_version $(VERSION)" --define "_rpmdir $(PWD)/$(DIST)" \
		--define "dist %{nil}" packaging/reclaim.spec >/dev/null
	@# rpmbuild files by architecture; flatten so every artifact is in one place.
	@find $(DIST) -mindepth 2 -name '*.rpm' -exec mv {} $(DIST)/ \;
	@rmdir $(DIST)/*/ 2>/dev/null || true
	@ls $(DIST)/*.rpm

clean:
	rm -rf $(DIST) reclaim
