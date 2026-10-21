BIN := bmux
INSTALL_PATH := /usr/local/bin/bmux

.PHONY: build update-local
build:
	go build -o $(BIN) ./cmd/bmux

update-local: build
	mv $(BIN) $(INSTALL_PATH)
