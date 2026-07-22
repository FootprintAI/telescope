BINARY := telescope
PKG := ./cmd/telescope

.PHONY: build test vet demo clean

build:
	go build -o $(BINARY) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

# Offline demo using the mock provider (no cloud creds needed).
demo: build
	./$(BINARY) scan --provider mock
	@echo
	./$(BINARY) scan --provider mock --output json --out /tmp/telescope-report.json
	@echo "wrote /tmp/telescope-report.json (the cloud-service handoff)"

clean:
	rm -f $(BINARY)
