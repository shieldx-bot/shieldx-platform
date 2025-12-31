# Image URL to use all building/pushing image targets
IMG ?= shieldxbot/controller:v0.0.20

# Go toolchain discovery (safe: does not modify your shell profile)
#
# This repo needs the `go` binary for controller-gen (loading packages), codegen, fmt/vet, tests, etc.
# Some environments install Go under /usr/local/go/bin but do not add it to $PATH.
GO ?= $(shell command -v go 2>/dev/null)
ifeq ($(GO),)
	ifneq ($(wildcard /usr/local/go/bin/go),)
		GO := /usr/local/go/bin/go
		export PATH := /usr/local/go/bin:$(PATH)
	endif
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: check-go
check-go: ## Verify that the Go toolchain is available.
	@command -v go >/dev/null 2>&1 || [ -x "/usr/local/go/bin/go" ] || { \
		echo "Error: Go toolchain not found in PATH."; \
		echo "  Detected common install path: /usr/local/go/bin/go (missing from PATH in your shell)."; \
		echo "  Fix options:"; \
		echo "    - Install Go (recommended), or"; \
		echo "    - Add /usr/local/go/bin to your PATH (shell profile), or"; \
		echo "    - Run make with PATH=\"/usr/local/go/bin:$$PATH\""; \
		exit 1; \
	}

.PHONY: manifests
manifests: check-go controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: check-go controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	$(GO) vet ./...

.PHONY: test
test: check-go manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" $(GO) test $$($(GO) list ./... | grep -v /e2e) -coverprofile cover.out

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= shieldx-platform-test-e2e

.PHONY: setup-test-e2e
setup-test-e2e: ## Set up a Kind cluster for e2e tests if it does not exist
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			$(KIND) create cluster --name $(KIND_CLUSTER) ;; \
	esac

.PHONY: test-e2e
test-e2e: check-go setup-test-e2e manifests generate fmt vet ## Run the e2e tests. Expected an isolated environment using Kind.
	KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) $(GO) test -tags=e2e ./test/e2e/ -v -ginkgo.v
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER)

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT)" config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	$(GO) build -o bin/manager cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	$(GO) run ./cmd/main.go

.PHONY: shieldctl
shieldctl: check-go ## Build shieldctl CLI into ./bin (does not install).
	$(GO) build -o bin/shieldctl ./cmd/shieldctl

.PHONY: install-shieldctl
install-shieldctl: shieldctl ## Install shieldctl to GOBIN (or GOPATH/bin). Refuses to overwrite unless FORCE=1.
	@set -e; \
	GOBIN="$$($(GO) env GOBIN)"; \
	if [ -z "$$GOBIN" ]; then GOBIN="$$($(GO) env GOPATH)/bin"; fi; \
	DST="$$GOBIN/shieldctl"; \
	if [ -e "$$DST" ] && [ "$(FORCE)" != "1" ]; then \
		echo "Refusing to overwrite existing $$DST"; \
		echo "Re-run with FORCE=1 to overwrite, or remove the existing file."; \
		exit 1; \
	fi; \
	mkdir -p "$$GOBIN"; \
	install -m 0755 bin/shieldctl "$$DST"; \
	echo "Installed shieldctl -> $$DST"

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .
	$(CONTAINER_TOOL) push ${IMG}
	make deploy
# 	go build -o ~/go/bin/shieldctl  ./cmd/shieldctl
# 	echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc
# 	echo "-------------------docker build success--------------------"
# 	echo "-------------------build success--------------------"
# 	$(KUBECTL) -n shieldx-platform-system rollout restart deployment/shieldx-platform-controller-manager
# 	kubectl rollout restart deploy/shieldx-platform-controller-manager -n shieldx-platform-system
# # 	echo "-------------------rollout success------------------"
# 	kubectl delete pod -n shieldx-platform-system myapp  --ignore-not-found=true
 # 	kubectl apply -f  /home/shieldx/Documents/GitHub/shieldx-platform/setup/webhook/test/pod.yaml
.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name shieldx-platform-builder
	$(CONTAINER_TOOL) buildx use shieldx-platform-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm shieldx-platform-builder
	rm Dockerfile.cross

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" apply -f -
  	# Optional dev smoke-test pod (uncomment if you really want it)
 
.PHONY: webhook-status
webhook-status: ## Print controller/webhook resources and recent webhook-related logs.
	@NS=shieldx-platform-system; \
	echo "## Deployments"; $(KUBECTL) -n $$NS get deploy -o wide; \
	echo; echo "## Pods"; $(KUBECTL) -n $$NS get pods -o wide; \
	echo; echo "## Webhook Service"; $(KUBECTL) -n $$NS get svc shieldx-platform-webhook-service -o wide; \
	echo; echo "## Endpoints"; $(KUBECTL) -n $$NS get endpoints shieldx-platform-webhook-service -o wide || true; \
	echo; echo "## EndpointSlices"; $(KUBECTL) -n $$NS get endpointslice -l kubernetes.io/service-name=shieldx-platform-webhook-service -o wide 2>/dev/null || true; \
	echo; echo "## WebhookConfigurations"; $(KUBECTL) get validatingwebhookconfigurations,mutatingwebhookconfigurations | grep -i shieldx || true; \
	echo; echo "## Recent manager logs (webhook lines)"; \
	POD=$$($(KUBECTL) -n $$NS get pods -l control-plane=controller-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true); \
	if [ -n "$$POD" ]; then \
		echo "Using pod: $$POD"; \
		$(KUBECTL) -n $$NS logs $$POD -c manager --tail=100 | egrep -i 'webhook|serving|listening|tls|cert|error' || true; \
	else \
		echo "No controller-manager pod found"; \
	fi

.PHONY: webhook-unblock
webhook-unblock: ## Temporarily set failurePolicy=Ignore to unblock kubectl apply when webhook is unreachable (DEV ONLY).
	@set -e; \
	MWH=shieldx-platform-mutating-webhook-configuration; \
	VWH=shieldx-platform-validating-webhook-configuration; \
	echo "Patching $$MWH failurePolicy=Ignore"; \
	$(KUBECTL) patch mutatingwebhookconfiguration $$MWH --type='json' \
		-p='[{"op":"replace","path":"/webhooks/0/failurePolicy","value":"Ignore"}]'; \
	echo "Patching $$VWH failurePolicy=Ignore"; \
	$(KUBECTL) patch validatingwebhookconfiguration $$VWH --type='json' \
		-p='[{"op":"replace","path":"/webhooks/0/failurePolicy","value":"Ignore"}]'; \
	echo "Done. Remember to run 'make webhook-block' after webhook connectivity is fixed."

.PHONY: webhook-block
webhook-block: ## Restore failurePolicy=Fail (recommended for production enforcement).
	@set -e; \
	MWH=shieldx-platform-mutating-webhook-configuration; \
	VWH=shieldx-platform-validating-webhook-configuration; \
	echo "Restoring $$MWH failurePolicy=Fail"; \
	$(KUBECTL) patch mutatingwebhookconfiguration $$MWH --type='json' \
		-p='[{"op":"replace","path":"/webhooks/0/failurePolicy","value":"Fail"}]'; \
	echo "Restoring $$VWH failurePolicy=Fail"; \
	$(KUBECTL) patch validatingwebhookconfiguration $$VWH --type='json' \
		-p='[{"op":"replace","path":"/webhooks/0/failurePolicy","value":"Fail"}]';

.PHONY: webhook-smoke
webhook-smoke: ## Server-side dry-run to verify admission webhooks (TLS + reachability + validation).
	@set -e; \
	echo "## Valid Tenant (should succeed)"; \
	printf '%s\n' \
	  'apiVersion: platform.shieldx.io/v1alpha1' \
	  'kind: Tenant' \
	  'metadata:' \
	  '  name: tenant-webhook-smoketest' \
	  'spec:' \
	  '  owners:' \
	  '    - admin@example.com' \
	  '  tier: basic' \
	  '  isolation: namespace' \
	| $(KUBECTL) apply --dry-run=server -f -; \
	echo; echo "## Invalid Tenant (should fail validation, not TLS/timeout)"; \
	TMP=$$(mktemp); \
	set +e; \
	printf '%s\n' \
	  'apiVersion: platform.shieldx.io/v1alpha1' \
	  'kind: Tenant' \
	  'metadata:' \
	  '  name: tenant-webhook-should-fail' \
	  'spec:' \
	  '  tier: basic' \
	  '  isolation: namespace' \
	| $(KUBECTL) apply --dry-run=server -f - 2>&1 | tee $$TMP; \
	RC=$$?; \
	set -e; \
	if [ $$RC -eq 0 ]; then \
		echo; echo "ERROR: Invalid Tenant unexpectedly succeeded."; \
		rm -f $$TMP; \
		exit 1; \
	fi; \
	if egrep -qi 'failed calling webhook|x509|tls:|timeout|no endpoints available' $$TMP; then \
		echo; echo "ERROR: Failure looks like webhook connectivity/TLS, not a validation error."; \
		rm -f $$TMP; \
		exit 1; \
	fi; \
	rm -f $$TMP; \
	echo; echo "OK: Invalid Tenant failed validation as expected."

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint

## Tool Versions
KUSTOMIZE_VERSION ?= v5.7.1
CONTROLLER_TOOLS_VERSION ?= v0.19.0

#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?([0-9]+)\.([0-9]+).*/release-\1.\2/')

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

GOLANGCI_LINT_VERSION ?= v2.5.0
.PHONY: kustomize
kustomize: check-go $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: check-go $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: check-go $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: check-go $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef
