.PHONY: build test test-example lint clean proxy-test gen-ca verify-proxy

BINARY=bin/gauntlet

build:
	go build -o $(BINARY) ./cmd/gauntlet

test:
	go test ./... -v -race -timeout 120s

proxy-test:
	go test ./internal/proxy/... -v -race -run TestProxy

test-example: build
	cd examples/support-agent && \
	pip install -r agent/requirements.txt -q && \
	pip install -e ../../sdk/python/ -q && \
	bash tests/test_integration.sh

lint:
	golangci-lint run ./... --timeout 5m

clean:
	rm -rf bin/ examples/support-agent/evals/runs/

gen-ca:
	$(BINARY) tls generate-ca

verify-proxy:
	go test ./internal/proxy/... -v -run TestProxyEndToEnd
