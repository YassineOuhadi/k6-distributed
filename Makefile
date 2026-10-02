CLUSTER  := k6-lab
SERVICES := gateway order payment inventory
BASE_URL ?= http://localhost:8080

.PHONY: up cluster build load deploy redeploy status smoke fault-payment heal-payment monitoring dashboards k6-operator k6-scripts k6-run k6-logs down

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
	helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1 || true
	helm upgrade --install tempo grafana/tempo -n monitoring -f deploy/monitoring/tempo.yaml --wait --timeout 5m
	kubectl apply -f deploy/monitoring/otel-collector.yaml
	kubectl -n monitoring rollout restart deploy/otel-collector
	kubectl -n monitoring rollout status deploy/otel-collector --timeout=120s
	$(MAKE) dashboards

dashboards: ## Load Grafana dashboards from deploy/monitoring/dashboards
	kubectl -n monitoring create configmap shop-dashboards --from-file=deploy/monitoring/dashboards \
		--dry-run=client -o yaml | kubectl label --local -f - grafana_dashboard=1 -o yaml | kubectl apply -f -

TEST        ?= load
PARALLELISM ?= 4

k6-operator: ## Install k6-operator
	helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1 || true
	helm upgrade --install k6-operator grafana/k6-operator -n k6-operator-system --create-namespace \
		-f deploy/k6/operator.yaml --wait --timeout 5m
	kubectl create namespace loadtest --dry-run=client -o yaml | kubectl apply -f -

k6-scripts: ## Upload k6/*.js as ConfigMap k6-scripts
	kubectl -n loadtest create configmap k6-scripts --from-file=k6 --dry-run=client -o yaml | kubectl apply -f -

# make k6-run TEST=load PARALLELISM=4
k6-run: k6-scripts ## Run k6/$(TEST).js in the cluster on $(PARALLELISM) pods
	kubectl -n loadtest delete testrun $(TEST) --ignore-not-found --wait
	sed -e 's/TESTID/$(TEST)-'$$(date +%Y%m%d-%H%M%S)'/' -e 's/PARALLELISM/$(PARALLELISM)/' -e 's/TEST/$(TEST)/g' \
		deploy/k6/testrun.yaml | kubectl apply -f -

k6-logs: ## Follow the runner pods of $(TEST)
	kubectl -n loadtest logs -f -l k6_cr=$(TEST),runner=true --max-log-requests 10 --prefix

down: ## Delete the cluster
	kind delete cluster --name $(CLUSTER)
