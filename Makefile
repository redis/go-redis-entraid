
test.ci:
	go test ./... -coverprofile=./cover.out -covermode=atomic -race -count 2 -timeout 5m

test:
	go test ./... -coverprofile=./cover.out -covermode=atomic -race -count 1
	go test ./... -bench=. -benchmem -count 1 -timeout 1m
