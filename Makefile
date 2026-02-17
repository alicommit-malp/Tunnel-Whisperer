BINARY := tw
CMD    := ./cmd/tw

.PHONY: build run clean proto

build:
	go build -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)

proto:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/api/v1/service.proto
