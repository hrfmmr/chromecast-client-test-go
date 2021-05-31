PROTOC := /usr/local/bin/protoc
PROTO_SRC_DIR := protobuf
PROTO_DEST_DIR := .

.PHONY: run
run:
	go run app.go

.PHONY: pb
pb:
	$(PROTOC) \
		-I $(PROTO_SRC_DIR) \
		--go_out $(PROTO_DEST_DIR) \
		$(PROTO_SRC_DIR)/cast_channel.proto
