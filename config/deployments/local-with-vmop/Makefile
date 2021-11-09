# Ensure Make is run with bash shell as some syntax below is bash-specific
SHELL := /usr/bin/env bash

.DEFAULT_GOAL := help

DEPENDENCY_COMPONENTS := dependency-components.yaml
INFRASTRUCTURE_COMPONENTS := infrastructure-components.yaml

## --------------------------------------
## Help
## --------------------------------------

help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)	

.PHONY: clean-infrastructure
clean-infrastructure: ## Remove generated files
	-rm $(INFRASTRUCTURE_COMPONENTS)

.PHONY: clean-dependencies
clean-dependencies: ## Remove generated dependency files
	-rm $(DEPENDENCY_COMPONENTS)

.PHONY: clean
clean: clean-infrastructure clean-dependencies ## Remove all generated files

## --------------------------------------
## Dependencies
## --------------------------------------
#DEPENDENCIES := certmanager

.PHONY: dependencies
dependencies: $(DEPENDENCIES) ## Generate the Kustomize configuration for all dependencies

$(DEPENDENCIES): clean-dependencies ## Generate the Kustomize configuration for dependencies
	#kustomize build $@ >>$(DEPENDENCY_COMPONENTS)
	#@echo "---" >>$(INFRASTRUCTURE_COMPONENTS)

## --------------------------------------
## Components
## --------------------------------------

# NOTE: CAPW is where we define the namespace for the full set, so it's
# important that it remains the first component built
COMPONENTS :=  capi capv vmoperator

.PHONY: infrastructure
infrastructure: $(COMPONENTS) ## Generate the Kustomize configuration for all infrastructure

$(COMPONENTS): clean-infrastructure ## Generate the Kustomize configuration for a component
	kustomize build $@  >>$(INFRASTRUCTURE_COMPONENTS)
	@echo "---" >>$(INFRASTRUCTURE_COMPONENTS)

.PHONY: all
#all: clean $(COMPONENTS) certmanager ## Generate all Kustomize configuration
all: clean $(COMPONENTS) ## Generate all Kustomize configuration
#all: clean $(COMPONENTS)
