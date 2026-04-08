BINARY := wt
VERSION ?= dev
BUILD_DIR := .
INSTALL_DIR := /usr/local/bin
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build install clean run tidy

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) .

run: build
	./$(BINARY)

install: build
	cp $(BUILD_DIR)/$(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed $(BINARY) to $(INSTALL_DIR)"

clean:
	rm -f $(BUILD_DIR)/$(BINARY)

tidy:
	go mod tidy
