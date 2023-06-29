SHELL:=/usr/bin/env bash
VERSION:=$(shell [ -z "$$(git tag --points-at HEAD)" ] && echo "$$(git describe --always --long --dirty | sed 's/^v//')" || echo "$$(git tag --points-at HEAD | sed 's/^v//')")
GO_FILES:=$(shell find . -name '*.go' | grep -v /vendor/)
BIN_NAME:=shopsonosnoisecontrol
SVCFILE_NAME=shopsonosnoisecontrol.service

default: help

# via https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
.PHONY: help
help: ## Print help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: clean
clean: ## Remove built products in ./out
	rm -rf ./out

.PHONY: lint
lint: ## Lint all .go files
	golangci-lint run *.go

.PHONY: build
build: ## Build (for the current platform & architecture) to ./out
	mkdir -p out
	go build -ldflags="-X main.version=${VERSION}" -o ./out/${BIN_NAME} .

# Note:
# "Pi1 is ARM6, Pi2 and 3 are ARM7, Pi4 is ARM8. However, commonly distributed OSs like Raspbian are still ARM7 in 32 bit mode.
# "While ARM supports both little and big endian operations, the RasPis run in little endian mode like x86 CPUs."
# - https://github.com/mlnoga/nightlight/issues/4
#
# See also: https://github.com/golang/go/wiki/GoArm
.PHONY: build-rpi
build-rpi: ## Build (for Raspberry Pi 2+, running a 32-bit OS) to ./out/rpi
	mkdir -p out/rpi
	env GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-X main.version=${VERSION}" -o ./out/rpi/${BIN_NAME} .

.PHONY: install
install: ## Install from ./out to /usr/local/bin (run with sudo)
	mkdir -p /usr/local/bin
	cp -i ./out/${BIN_NAME} /usr/local/bin
	chown root:root /usr/local/bin/${BIN_NAME}
	chmod +x /usr/local/bin/${BIN_NAME}

.PHONY: install-rpi
install-rpi: ## Install from ./out/rpi to /usr/local/bin (run with sudo)
	mkdir -p /usr/local/bin
	cp -i ./out/rpi/${BIN_NAME} /usr/local/bin
	chown root:root /usr/local/bin/${BIN_NAME}
	chmod +x /usr/local/bin/${BIN_NAME}

.PHONY: install-systemd
install-systemd: ## Install the systemd service (run with sudo)
	@echo "Installing systemd service in /etc/systemd/system; customize it there and reload the systemd daemon."
	@echo " > sudo nano /etc/systemd/system/${SVCFILE_NAME}"
	@echo " > sudo systemctl daemon-reload"
	@echo " > sudo systemctl enable ${SVCFILE_NAME}"
	cp -i ./dist/${SVCFILE_NAME} /etc/systemd/system
	chown root:root /etc/systemd/system/${SVCFILE_NAME}
