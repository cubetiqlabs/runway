.PHONY: dev-api dev-web build-web embed-web build build-agent test vet docker-build up down deploy-init deploy backup restore release

BINARY := bin/server
AGENT_BINARY := bin/runway-agent

dev-api:
	go run ./cmd/server

dev-web:
	cd web && bun run dev

build-web:
	cd web && bun run generate-routes && bun run typecheck && bun run build

embed-web: build-web
	rm -rf internal/webui/dist && mkdir -p internal/webui/dist
	cp -R web/dist/client/* internal/webui/dist/

build: embed-web
	CGO_ENABLED=0 go build -trimpath -tags netgo,osusergo -ldflags="-s -w" -o $(BINARY) ./cmd/server

build-agent:
	CGO_ENABLED=0 go build -trimpath -tags netgo,osusergo -ldflags="-s -w" -o $(AGENT_BINARY) ./cmd/agent

vet:
	go vet ./cmd/... ./internal/...

test:
	go test -race -v ./cmd/... ./internal/...

docker-build:
	DOCKER_BUILDKIT=1 docker build -t ghcr.io/cubetiqlabs/runway:latest .

up:
	docker compose up -d --build --wait

down:
	docker compose down

deploy-init:
	./scripts/deploy.sh init

deploy:
	./scripts/deploy.sh deploy --mode build --prod

backup:
	./scripts/deploy.sh backup

restore:
	@test -n "$(FILE)" || (echo "Usage: make restore FILE=backups/runway_....dump" && exit 1)
	./scripts/deploy.sh restore "$(FILE)"

release:
	@test -n "$(VERSION)" || (echo "Usage: make release VERSION=v0.1.0" && exit 1)
	./scripts/release.sh "$(VERSION)"
