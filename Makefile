#!/usr/bin/make -f

SHELL=bash
# GOBIN is used only to install neo-go and allows to override
# the location of written binary.
export GOBIN ?= $(shell pwd)/bin
NEOGO ?= $(GOBIN)/cli
VERSION ?= $(shell git describe --tags --dirty --match "v*" --always --abbrev=8 2>/dev/null || cat VERSION 2>/dev/null || echo "develop")


# .deb package versioning
OS_RELEASE = $(shell lsb_release -cs)
PKG_VERSION ?= $(shell echo $(VERSION) | sed "s/^v//" | \
			sed -E "s/(.*)-(g[a-fA-F0-9]{6,8})(.*)/\1\3~\2/" | \
			sed "s/-/~/")-${OS_RELEASE}

.PHONY: all build clean test neo-go
.PHONY: alphabet mainnet morph nns sidechain
.PHONY: debpackage debclean
build: neo-go all
all: sidechain mainnet
sidechain: alphabet morph nns

alphabet_sc = alphabet
morph_sc = audit balance container frostfsid netmap proxy reputation subnet
mainnet_sc = frostfs processing
nns_sc = nns

define sc_template
$(2)$(1)/$(1)_contract.nef: $(2)$(1)/$(1)_contract.go
	$(NEOGO) contract compile -i $(2)$(1) -c $(if $(2),$(2),$(1)/)config.yml -m $(2)$(1)/config.json -o $(2)$(1)/$(1)_contract.nef

$(if $(2),$(2)$(1)/$(1)_contract.go: alphabet/alphabet.go alphabet/alphabet.tpl
	go run alphabet/alphabet.go
)
endef

$(foreach sc,$(alphabet_sc),$(eval $(call sc_template,$(sc))))
$(foreach sc,$(morph_sc),$(eval $(call sc_template,$(sc))))
$(foreach sc,$(mainnet_sc),$(eval $(call sc_template,$(sc))))
$(foreach sc,$(nns_sc),$(eval $(call sc_template,$(sc))))

alphabet: $(foreach sc,$(alphabet_sc),$(sc)/$(sc)_contract.nef)
morph: $(foreach sc,$(morph_sc),$(sc)/$(sc)_contract.nef)
mainnet: $(foreach sc,$(mainnet_sc),$(sc)/$(sc)_contract.nef)
nns: $(foreach sc,$(nns_sc),$(sc)/$(sc)_contract.nef)

neo-go:
	@go list -f '{{.Path}}/...@{{.Version}}' -m github.com/nspcc-dev/neo-go \
		| xargs go install -v

test:
	@go test ./tests/...

clean:
	find . -name '*.nef' -exec rm -rf {} \;
	find . -name 'config.json' -exec rm -rf {} \;
	rm -rf ./bin/

mr_proper: clean
	for sc in $(alphabet_sc); do\
	  rm -rf alphabet/$$sc; \
	done

archive: build
	@tar --transform "s|^./|frostfs-contract-$(VERSION)/|" \
		-czf frostfs-contract-$(VERSION).tar.gz \
		$(shell find . -name '*.nef' -o -name 'config.json')

# Package for Debian
debpackage:
	dch --package frostfs-contract \
			--controlmaint \
			--newversion $(PKG_VERSION) \
			--distribution $(OS_RELEASE) \
			"Please see CHANGELOG.md for code changes for $(VERSION)"
	dpkg-buildpackage --no-sign -b

debclean:
	dh clean		
