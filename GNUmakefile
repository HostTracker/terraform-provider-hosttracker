TFPLUGINDOCS_VERSION ?= v0.23.0
TERRAFORM_VERSION ?= 1.9.8
TOOLS_DIR := $(CURDIR)/.tools

default: build

# Compile the provider into the working directory.
build:
	go build -o terraform-provider-hosttracker .

# Unit tests. They need no terraform binary and no credential.
test:
	go test ./... -count=1

# Acceptance tests. They create and delete real monitors on the account the
# token belongs to, and they need a terraform binary on PATH (or in
# .tools/, see the terraform target).
testacc:
	TF_ACC=1 go test ./internal/provider/... -count=1 -v -timeout 30m

fmt:
	gofmt -w .
	terraform fmt -recursive ./examples

vet:
	go vet ./...

lint: vet
	gofmt -l . | tee /dev/stderr | (! read)
	terraform fmt -check -recursive ./examples

# Regenerate docs/ from the schemas, the templates and the examples. A terraform binary must be reachable:
# .tools/ (populated by `make tools`) is preferred - without one on PATH, tfplugindocs downloads its own.
docs:
	PATH="$(CURDIR)/.tools:$$PATH" go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$(TFPLUGINDOCS_VERSION) generate --provider-name hosttracker

docs-check:
	PATH="$(CURDIR)/.tools:$$PATH" go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$(TFPLUGINDOCS_VERSION) validate --provider-name hosttracker

# Fetch a terraform binary into .tools/ for the acceptance tests, rather
# than installing one system-wide.
terraform:
	mkdir -p $(TOOLS_DIR)
	curl -sSLo $(TOOLS_DIR)/terraform.zip \
		https://releases.hashicorp.com/terraform/$(TERRAFORM_VERSION)/terraform_$(TERRAFORM_VERSION)_linux_amd64.zip
	cd $(TOOLS_DIR) && unzip -o terraform.zip terraform && rm terraform.zip
	@echo "add $(TOOLS_DIR) to PATH before running make testacc"

.PHONY: default build test testacc fmt vet lint docs docs-check terraform
