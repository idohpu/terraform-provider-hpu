# default: testacc

# # Run acceptance tests
# .PHONY: testacc
# testacc:
# 	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m
build: #clean, install, and update the terraform files
	go mod tidy
# bash config/build_provider