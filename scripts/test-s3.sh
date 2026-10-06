#!/usr/bin/env bash
set -euo pipefail

# MinIO withdrew its public community images and binaries in October 2026, so
# the S3 storage contract runs against pinned SeaweedFS instead. The digest is
# the multi-arch index, covering both linux/amd64 and linux/arm64.
image='chrislusf/seaweedfs@sha256:1055999e08eed1789b0ae45d235126e4495e23d3fb9d6396293fd42539b1ae6a'
container="depsilo-s3-contract-$$"
access_key='depsilo-test-access'
secret_key='depsilo-test-secret-key-0123456789'
bucket="depsilo-contract-$$"

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT

s3_config=$(printf '{"identities":[{"name":"depsilo-contract","credentials":[{"accessKey":"%s","secretKey":"%s"}],"actions":["Admin","Read","Write","List","Tagging"]}]}' \
  "$access_key" "$secret_key")

docker run --detach --rm \
  --name "$container" \
  --publish 127.0.0.1::8333 \
  --env "S3_CONFIG_JSON=$s3_config" \
  --entrypoint sh \
  "$image" -c 'printf %s "$S3_CONFIG_JSON" > /etc/seaweedfs/s3.json && exec weed server -dir=/data -s3 -s3.port=8333 -s3.config=/etc/seaweedfs/s3.json -master.volumeSizeLimitMB=16' >/dev/null

port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "8333/tcp") 0).HostPort}}' "$container")
endpoint="http://127.0.0.1:$port"

ready=false
for _ in $(seq 1 100); do
  # With static credentials configured, an unauthenticated probe answers 403
  # once the gateway is listening; any HTTP status means it accepted the
  # connection.
  status=$(curl --silent --output /dev/null --write-out '%{http_code}' "$endpoint/" || true)
  if [[ "$status" != '000' ]]; then
    ready=true
    break
  fi
  sleep 0.2
done
if [[ "$ready" != true ]]; then
  docker logs "$container" >&2
  echo 'SeaweedFS did not become ready' >&2
  exit 1
fi

DEPSILO_STORAGE_TYPE='s3' \
DEPSILO_STORAGE_ENDPOINT="$endpoint" \
DEPSILO_STORAGE_BUCKET="$bucket" \
DEPSILO_STORAGE_REGION='us-east-1' \
DEPSILO_STORAGE_ACCESS_KEY="$access_key" \
DEPSILO_STORAGE_SECRET_KEY="$secret_key" \
  go test -tags=s3integration ./internal/cache -run '^TestS3StorageContract$' -count=1
