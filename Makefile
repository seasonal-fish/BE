TEST_RESULTS := test-results

.PHONY: build vet test cover test-junit test-integration run tidy

build:
	go build ./...

vet:
	go vet ./...

# 유닛 테스트 (DB·OpenAI 불필요)
test:
	go test ./...

# 커버리지 요약
cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

# 평가용 JUnit XML 리포트 생성 → test-results/junit.xml (+ coverage.out)
# 필요: go install gotest.tools/gotestsum@latest
test-junit:
	mkdir -p $(TEST_RESULTS)
	gotestsum --junitfile $(TEST_RESULTS)/junit.xml --format testname -- -coverprofile=coverage.out ./...

# 통합 테스트 (TEST_DATABASE_URL 설정 시 실제 DB 사용, 없으면 skip)
test-integration:
	go test -tags=integration ./...

run:
	go run ./cmd/server

tidy:
	go mod tidy
