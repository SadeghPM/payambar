.PHONY: build build-frontend build-backend build-all clean run dev docker-build docker-run test fmt

# Build frontend to cmd/payambar/static/
build-frontend:
	@echo "Building frontend..."
	mkdir -p cmd/payambar/static
	cp frontend/index.html cmd/payambar/static/
	cp frontend/styles.css cmd/payambar/static/
	cp frontend/app.js cmd/payambar/static/
	cp frontend/manifest.json cmd/payambar/static/
	cp frontend/sw.js cmd/payambar/static/
	if [ -f frontend/vue.global.prod.js ]; then cp frontend/vue.global.prod.js cmd/payambar/static/; fi
	if [ -d frontend/fonts ]; then cp -R frontend/fonts cmd/payambar/static/; fi
	# PWA icons
	cp frontend/favicon.svg cmd/payambar/static/
	cp frontend/favicon-96.png cmd/payambar/static/
	cp frontend/favicon-192.png cmd/payambar/static/
	cp frontend/favicon-512.png cmd/payambar/static/
	cp frontend/favicon-maskable-192.png cmd/payambar/static/
	cp frontend/favicon-maskable-512.png cmd/payambar/static/
	cp frontend/apple-touch-icon.png cmd/payambar/static/
	cp frontend/screenshot-540.png cmd/payambar/static/
	cp frontend/screenshot-1280.png cmd/payambar/static/
	# Inject build hash into sw.js so browsers detect frontend changes
	$(eval BUILD_HASH := $(shell cat frontend/index.html frontend/styles.css frontend/app.js frontend/sw.js | shasum -a 256 | cut -c1-12))
	sed -i.bak "s/__BUILD_HASH__/$(BUILD_HASH)/" cmd/payambar/static/sw.js && rm -f cmd/payambar/static/sw.js.bak
	@echo "Frontend built in cmd/payambar/static/ (hash: $(BUILD_HASH))"

# Build backend with embedded frontend (current OS)
build-backend: build-frontend
	@echo "Building backend..."
	mkdir -p bin
	go build -o bin/payambar ./cmd/payambar


# Build all (current OS only)
build-all: build-backend
	@echo "Build complete: bin/payambar"


# Run locally
run: build-backend
	PORT=8080 DATABASE_PATH=/tmp/payambar.db JWT_SECRET=dev-secret-key bin/payambar

# Dev (with frontend assets copied)
dev: build-frontend
	PORT=8080 DATABASE_PATH=./data/payambar.db JWT_SECRET=dev-secret-key \
	VAPID_PUBLIC_KEY=BK-m223f6sYwqN2cgyv7e5HSLMlXqEUyPuPUz4LVwlqVsjWQVLe7d_Gi9LVVtzb37yv1pPv9kbqiRFheGlcCOnk \
	VAPID_PRIVATE_KEY=xDf-gMEdudmVDlRDY5B5u9p6u2Yte_r78_sjm0BOdoY \
	go run ./cmd/payambar

# Clean
clean:
	rm -rf bin/
	rm -rf cmd/payambar/static
	rm -rf data/

# Docker build
docker-build:
	docker build -t payambar:latest .

# Docker run
docker-run:
	docker run -p 8080:8080 \
		-e DATABASE_PATH=/data/payambar.db \
		-e JWT_SECRET=your-secret-key \
		-v payambar_data:/data \
		payambar:latest

# Run all tests
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	@echo "Coverage report:"
	go tool cover -func=coverage.out


# Format code
fmt:
	@echo "Formatting code..."
	gofmt -w -s .
