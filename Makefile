.PHONY: test baseline gen fixtures collector inspect fmt vet ci

test:
	go test ./...

baseline:
	go run ./scripts/baseline.go

fixtures:
	go run ./cmd/genfixtures -out testdata

collector:
	go run ./cmd/collector -once -dir inbox -out parsed

inspect:
	@test -n "$(FILE)" || (echo "用法: make inspect FILE=testdata/raw/xxx.xlsx"; exit 2)
	go run ./cmd/inspect -file $(FILE)

fmt:
	gofmt -l -w .

vet:
	go vet ./...

ci: vet test
