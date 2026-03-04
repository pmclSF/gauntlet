.PHONY: build test test-example test-pydantic-real test-openai-real test-real-repos lint clean proxy-test gen-ca verify-proxy

BINARY=bin/gauntlet

build:
	go build -o $(BINARY) ./cmd/gauntlet

test:
	go test ./... -v -race -timeout 120s

proxy-test:
	go test ./internal/proxy/... -v -race -run TestProxy

test-example: build
	cd examples/support-agent && \
	python3 -m pip install -r agent/requirements.txt -q && \
	python3 -m pip install -e ../../sdk/python/ -q && \
	python3 -m pip install pyyaml pytest -q && \
	bash tests/test_integration.sh

test-pydantic-real: build
	cd examples/pydantic-bank-real && \
	bash tests/test_integration.sh

test-openai-real: build
	cd examples/openai-agents-real && \
	bash tests/test_integration.sh

test-real-repos: test-pydantic-real test-openai-real

lint:
	golangci-lint run ./... --timeout 5m

clean:
	rm -rf bin/ examples/support-agent/evals/runs/

gen-ca:
	$(BINARY) tls generate-ca

verify-proxy:
	go test ./internal/proxy/... -v -run TestProxyEndToEnd
