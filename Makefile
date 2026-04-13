BINARY  = wizardry
PREFIX  ?= /usr/local

.PHONY: build install uninstall clean

build:
	go build -o $(BINARY) ./cmd/wizardry/

install: build
	install -d $(PREFIX)/bin
	install -m 755 $(BINARY) $(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(PREFIX)/bin/$(BINARY)

clean:
	rm -f $(BINARY)
