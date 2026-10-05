# argocd-lab-app

Source code and CI for the Argo CD GitOps lab. Deployment manifests live in [argocd-lab-gitops](https://github.com/MykhailoKhil/argocd-lab-gitops).

## demo-api

A small Go HTTP service (stdlib only) built to be released with canaries:

| endpoint   | purpose |
|------------|---------|
| `/`        | JSON response with version and pod name |
| `/healthz` | liveness |
| `/readyz`  | readiness |
| `/metrics` | Prometheus: `http_requests_total{path,code}`, `http_request_duration_seconds` histogram, `demo_api_build_info{version}` |

Fault injection, used to produce a "bad" release that canary analysis should reject:

| env var            | effect |
|--------------------|--------|
| `FAULT_LATENCY_MS` | adds latency to every request on `/` |
| `FAULT_ERROR_RATE` | share of requests on `/` that return 500 (0.0–1.0) |

Run locally:

```bash
cd services/demo-api
go test ./...
FAULT_ERROR_RATE=0.2 go run .
curl localhost:8080/ ; curl -s localhost:8080/metrics | grep http_requests_total
```

## CI

`.github/workflows/ci.yaml` runs tests on every PR. On push to `main` it builds a multi-arch image and pushes `ghcr.io/mykhailokhil/demo-api:sha-<short>`. A git tag `v1.2.3` also pushes `1.2.3` and `1.2`.

After the first successful run, open the package settings on GitHub and make the `demo-api` package public, so the clusters can pull it without credentials.
