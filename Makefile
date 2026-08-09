.PHONY: build test check live-test

build:
	go build -o monarch .

test:
	go test ./...

# live-test hits the REAL Monarch API using the saved Keychain session
# (read-only). Requires `monarch login` first. Never run in CI.
live-test:
	MONARCH_LIVE=1 go test ./pkg/monarch/ -run TestLive -v

# check is mandatory before commit and whenever go.mod/go.sum change.
# govulncheck runs via `go run …@latest` so it never enters go.mod.
check:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go mod tidy -diff
	go vet ./...
	go build -o /dev/null .
	go test ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
