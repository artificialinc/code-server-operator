# Local Development

1. `./local_development/local_up.sh`
2. `kubectl cluster-info --context kind-development`
3. `kubectl apply -f local_development/install-nginx.yaml`
4. `make install`
5. `make run`
6. `kubectl apply -f example/sample_codeserver.yaml`
