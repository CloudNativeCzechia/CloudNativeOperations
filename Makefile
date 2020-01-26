build-go:
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-s -w" -installsuffix cgo -o ./cmd/proxy/proxy ./cmd/proxy/proxy.go
	CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-s -w" -installsuffix cgo -o ./cmd/gotiny/tiny ./cmd/gotiny/tiny.go

build-docker:
	docker build -t docker.io/pkotas/gotiny-app:master ./cmd/gotiny/
	docker build -t docker.io/pkotas/gotiny-proxy:master ./cmd/proxy/

push-docker:
	docker push docker.io/pkotas/gotiny-app:master
	docker push docker.io/pkotas/gotiny-proxy:master

build: build-go build-docker

cluster-deploy:
	kubectl apply -f ./manifests/gotiny_dpl.yaml
	kubectl apply -f ./manifests/gotiny_svc.yaml
	kubectl apply -f ./manifests/proxy_dpl.yaml
	kubectl apply -f ./manifests/proxy_svc.yaml
	kubectl apply -f ./manifests/redis_dpl.yaml
	kubectl apply -f ./manifests/redis_svc.yaml

cluster-clean:
	kubectl delete -f ./manifests/gotiny_dpl.yaml
	kubectl delete -f ./manifests/gotiny_svc.yaml
	kubectl delete -f ./manifests/proxy_dpl.yaml
	kubectl delete -f ./manifests/proxy_svc.yaml
	kubectl delete -f ./manifests/redis_dpl.yaml
	kubectl delete -f ./manifests/redis_svc.yaml

clean-bin:
	rm ./cmd/gotiny/tiny
	rm ./cmd/proxy/proxy

clean: clean-bin cluster-clean

phony:
	build-docker build-go cluster-deploy clean-bin build
