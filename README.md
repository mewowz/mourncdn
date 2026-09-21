# MournCDN

MournCDN is a small project for deploying a CDN quickly to serve & consume assets. This was originally written for [mourn](https://mourn.bio) and serving their users.

## Install
### Download
Download by:
```
git clone https://github.com/mewowz/mourncdn
cd mourncdn
```
### Configure
Configure the server by editing `config.yml`. Most defaults will not need to be adjusted.
Please set your public key for the capabilities signing server under the `auth` section of the config
```
auth:
  public-key: Set your base64-encoded 32-byte Ed25519 public key here
```
### Running
After configuring, there are two ways to run the server:
1. Docker Compose (preferred)
```
docker compose up -d
```
2. Locally
```
mkdir -p ./data/tmp ./data/assets
CGO_ENABLED=0 go build -o mourncdn .
./mourncdn
```
After this, the server will be listening for incoming connections (default on port 0.0.0.0:9743)

## Metrics
MournCDN uses [Prometheus](https://prometheus.io/) for its internal metrics server. By default, this is exposed on localhost:9884.
If run with Docker, the metrics server is unavailable to the host. To access from outside of the Docker container, change the `ports` section of the `docker-compose.yaml` to
```
ports:
  - 9743:9743
  - 127.0.0.1:9884:9884
```
And in `config.yml`, change the `metrics.address` field to:
```
metrics:
  address: 0.0.0.0:9884
```
## Uploading
Uploads require a signed capability in the Authorization header. The request body contains the raw asset bytes.

## Performance
**10.9 Gbps sustained egress** over 60 seconds · **78.1 GiB streamed** in the large-file run

| Asset size | Completed GETs | Egress | p95 latency |
|---|---:|---:|---:|
| 50 MiB | 1,569 in 60s | 10.9 Gbps | 1.00s |
| 100 MiB | 777 in 61s | 10.7 Gbps | 1.97s |
| 1,000 MiB | 80 in 81s | 8.28 Gbps | 24.1s |

*Single-node [hey](https://github.com/rakyll/hey) tests with 20 concurrent requests, repeatedly fetching one asset. The 50 and 100 MiB assets were served from the application cache. The
1,000 MiB asset bypassed it but may have benefited from the OS page cache.*
