#!/usr/bin/env bash
set -euo pipefail

# MinIO withdrew its public community images and binaries in October 2026, so
# the S3 storage contract runs against pinned RustFS instead. The digest is the
# multi-arch index, covering both linux/amd64 and linux/arm64.
image='rustfs/rustfs@sha256:1803faef57627e2d9c2e7d89d655d712ddded5389040054987163043fecb6a3c'
container="depsilo-s3-contract-$$"
access_key='depsilo-test-access'
secret_key='depsilo-test-secret-key-0123456789'
bucket="depsilo-contract-$$"

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run --detach --rm \
  --name "$container" \
  --publish 127.0.0.1::9000 \
  --env "RUSTFS_ACCESS_KEY=$access_key" \
  --env "RUSTFS_SECRET_KEY=$secret_key" \
  "$image" server /data >/dev/null

port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "9000/tcp") 0).HostPort}}' "$container")
endpoint="http://127.0.0.1:$port"

ready=false
# RustFS starts the object store and S3 API in one process; on a cold CI runner
# it can take noticeably longer than on a warm developer machine to answer.
for _ in $(seq 1 240); do
  status=$(curl --silent --output /dev/null --write-out '%{http_code}' "$endpoint/health" || true)
  if [[ "$status" == '200' ]]; then
    ready=true
    break
  fi
  sleep 0.5
done
if [[ "$ready" != true ]]; then
  docker logs "$container" >&2
  echo 'RustFS did not become ready' >&2
  exit 1
fi

DEPSILO_STORAGE_TYPE='s3' \
DEPSILO_STORAGE_ENDPOINT="$endpoint" \
DEPSILO_STORAGE_BUCKET="$bucket" \
DEPSILO_STORAGE_REGION='us-east-1' \
DEPSILO_STORAGE_ACCESS_KEY="$access_key" \
DEPSILO_STORAGE_SECRET_KEY="$secret_key" \
  go test -tags=s3integration ./internal/cache -run '^TestS3StorageContract$' -count=1
