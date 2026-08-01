.PHONY: build tidy docker docker-push helm-lint install uninstall

IMAGE ?= kubeevents:latest
NAMESPACE ?= kubeevents
RELEASE ?= kubeevents

tidy:
	go mod tidy

build: tidy
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/kubeevents ./cmd/kubeevents

docker:
	docker build -t $(IMAGE) .

docker-push: docker
	docker push $(IMAGE)

helm-lint:
	helm lint deploy/helm/kubeevents

# Install with Helm (preferred)
install:
	helm upgrade --install $(RELEASE) deploy/helm/kubeevents \
		--namespace $(NAMESPACE) --create-namespace \
		--set telegram.botToken="$(TELEGRAM_BOT_TOKEN)" \
		--set telegram.chatId="$(TELEGRAM_CHAT_ID)" \
		--set clusterName="$(CLUSTER_NAME)" \
		--set image.repository="$(shell echo $(IMAGE) | cut -d: -f1)" \
		--set image.tag="$(shell echo $(IMAGE) | cut -d: -f2)"

# Install with plain manifests (edit secret first)
install-manifests:
	kubectl apply -f deploy/kubernetes/namespace.yaml
	kubectl apply -f deploy/kubernetes/rbac.yaml
	kubectl apply -f deploy/kubernetes/secret.yaml
	kubectl apply -f deploy/kubernetes/configmap.yaml
	kubectl apply -f deploy/kubernetes/deployment.yaml

uninstall:
	helm uninstall $(RELEASE) --namespace $(NAMESPACE) || true
	kubectl delete -f deploy/kubernetes/ --ignore-not-found=true
