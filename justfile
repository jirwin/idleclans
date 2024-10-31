# justfile

# Define the debug target
debug:
	rm -rf dist
	mkdir dist
	go build -gcflags="all=-N -l" -o dist/idleclans-debug ./cmd/idleclans-bot
	dlv exec "dist/idleclans-debug" --listen=127.0.0.1:2345 --headless --api-version=2 --accept-multiclient

run:
	go run ./cmd/idleclans-bot