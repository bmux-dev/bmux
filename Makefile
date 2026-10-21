BIN := bmux
INSTALL_PATH := /usr/local/bin/bmux

.PHONY: update-local
update-local:
	go build -o $(BIN) ./cmd/bmux
	mv $(BIN) $(INSTALL_PATH)
