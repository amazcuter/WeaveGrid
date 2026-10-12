.PHONY: frontend build run test smoke live-smoke notices source-check source-audit-git source-export

SOURCE_ARCHIVE ?= dist/weavegrid-source.tar.gz

frontend:
	cd web && npm ci && npm run build

build: frontend
	go build -trimpath -o bin/weavegrid ./cmd/weavegrid
	python3 scripts/collect_licenses.py

run: build
	./bin/weavegrid --addr 127.0.0.1:8731 --data data

test: frontend
	go test -race ./...
	go vet ./...
	cd web && npm test

smoke:
	go test ./internal/workbench -count=1

live-smoke:
	NOVELFLOW_LIVE=1 go test ./internal/workbench -run TestLiveOpenCode -count=1 -v

notices:
	python3 scripts/collect_licenses.py

source-check:
	python3 scripts/source_release.py check

source-audit-git:
	python3 scripts/source_release.py audit-git

source-export:
	python3 scripts/source_release.py export --output "$(SOURCE_ARCHIVE)"
