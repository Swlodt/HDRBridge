.PHONY: web build run dev-web test clean

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 go build -trimpath -o bin/hdrbridge ./cmd/hdrbridge

run: build
	./bin/hdrbridge

dev-web:
	cd web && npm run dev

test: web
	cd web && npm test
	go test -race ./...
	go vet ./...

clean:
	$(RM) -r bin/hdrbridge web/dist
