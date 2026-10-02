CLUSTER  := k6-lab
SERVICES := gateway order payment inventory
BASE_URL ?= http://localhost:8080

.PHONY: up cluster build load deploy redeploy status smoke fault-payment heal-payment monitoring down

up: cluster build load deploy ## Create cluster, build images, deploy everything

cluster: ## Create the kind cluster
	@kind get clusters | grep -qx $(CLUSTER) || kind create cluster --config deploy/kind/cluster.yaml

build: ## Build one image per service
	@for s in $(SERVICES); do docker build -q --build-arg SERVICE=$$s -t shop/$$s:dev services || exit 1; done

load: ## Push local images into the kind nodes
	kind load docker-image --name $(CLUSTER) $(addprefix shop/,$(addsuffix :dev,$(SERVICES)))

deploy: ## Apply manifests and wait for rollout
	kubectl apply -f deploy/k8s/shop.yaml
	kubectl -n shop rollout status deploy --timeout=120s

redeploy: build load ## Rebuild images and restart pods after a code change
	kubectl -n shop rollout restart deploy
	kubectl -n shop rollout status deploy --timeout=120s

status:
	kubectl -n shop get pods -o wide

smoke: ## Run the k6 smoke test
	k6 run -e BASE_URL=$(BASE_URL) k6/smoke.js

# Fault injection: e.g. make fault-payment FAULT='{"latency_ms":800,"error_rate":0.3}'
FAULT ?= {"error_rate":0.5}
fault-payment:
	@kubectl -n shop port-forward deploy/payment-service 18082:8080 >/dev/null & PF=$$!; sleep 2; \
	curl -s -XPOST localhost:18082/admin/fault -d '$(FAULT)'; echo; kill $$PF

heal-payment:
	@kubectl -n shop port-forward deploy/payment-service 18082:8080 >/dev/null & PF=$$!; sleep 2; \
	curl -s -XDELETE localhost:18082/admin/fault; echo; kill $$PF

monitoring:
	helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
	helm upgrade --install kps prometheus-community/kube-prometheus-stack \
		-n monitoring --create-namespace -f deploy/monitoring/kube-prometheus-stack.yaml --wait --timeout 10m
	kubectl apply -f deploy/monitoring/otel-collector.yaml
	kubectl -n monitoring rollout status deploy/otel-collector --timeout=120s

down: ## Delete the cluster
	kind delete cluster --name $(CLUSTER)
