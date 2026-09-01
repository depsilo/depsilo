#!/usr/bin/env bash
set -euo pipefail

image='quay.io/minio/minio:RELEASE.2025-09-07T16-13-09Z@sha256:14cea493d9a34af32f524e538b8346cf79f3321eff8e708c1e2960462bd8936e'
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
  --env "MINIO_ROOT_USER=$access_key" \
  --env "MINIO_ROOT_PASSWORD=$secret_key" \
  "$image" server /data >/dev/null

port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "9000/tcp") 0).HostPort}}' "$container")
endpoint="http://127.0.0.1:$port"

ready=false
for _ in $(seq 1 100); do
  if curl --fail --silent "$endpoint/minio/health/ready" >/dev/null; then
    ready=true
    break
  fi
  sleep 0.2
done
if [[ "$ready" != true ]]; then
  docker logs "$container" >&2
  echo 'MinIO did not become ready' >&2
  exit 1
fi

DEPSILO_STORAGE_TYPE='s3' \
DEPSILO_STORAGE_ENDPOINT="$endpoint" \
DEPSILO_STORAGE_BUCKET="$bucket" \
DEPSILO_STORAGE_REGION='us-east-1' \
DEPSILO_STORAGE_ACCESS_KEY="$access_key" \
DEPSILO_STORAGE_SECRET_KEY="$secret_key" \
  go test -tags=s3integration ./internal/cache -run '^TestS3StorageContract$' -count=1
