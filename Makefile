TAG = 1.0.0
DOCKERORG = aerogear
OPERATOR_IMAGE_NAME = managed-service-operator

.phony: build_operator_image
build_operator_image: build_operator_binary
	operator-sdk build $(DOCKERORG)/$(OPERATOR_IMAGE_NAME):$(TAG)

.phony: build_operator_binary
build_operator_binary:
	env GOOS=linux GOARCH=amd64 go build -o ./cmd/operator/operator ./cmd/operator
