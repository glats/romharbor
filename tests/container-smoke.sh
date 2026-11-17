#!/bin/sh
set -eu

image=${1:?Usage: sh tests/container-smoke.sh IMAGE}
scratch=$(mktemp -d "${TMPDIR:-/tmp}/romharbor-container.XXXXXX")
name="romharbor-smoke-$(basename "$scratch" | tr '[:upper:]' '[:lower:]')"
volume="$name-data"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker volume rm "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

docker run --rm --entrypoint sh "$image" -c '
  test "$(id -u)" = 10001
  test -s /etc/ssl/certs/ca-certificates.crt
'
start() {
  docker run -d --name "$name" --read-only --tmpfs /tmp \
    --cap-drop ALL --security-opt no-new-privileges:true \
    --mount "type=volume,source=$volume,target=/data" \
    -p 127.0.0.1::8080 "$image" > "$scratch/container-id"
  port=$(docker inspect --format '{{(index (index .NetworkSettings.Ports "8080/tcp") 0).HostPort}}' "$name")
  url="http://127.0.0.1:$port"
  curl --fail --silent --show-error --retry 20 --retry-connrefused \
    --retry-delay 1 --retry-max-time 60 --max-time 5 "$url/users" > "$scratch/users.html"
}
start
status=$(curl --silent --show-error --output "$scratch/rejected.json" \
  --write-out '%{http_code}' "$url/api/games?platform=..%2Foutside")
test "$status" = 400
grep -F '"error":"unknown platform"' "$scratch/rejected.json"
status=$(curl --silent --show-error --output "$scratch/created.html" \
  --write-out '%{http_code}' --data 'name=ContainerSmoke&email=smoke@example.invalid' "$url/users")
test "$status" = 201
docker exec "$name" sh -c 'test -s /data/app.db && test -w /data/roms && test -w /data/romcache && test ! -e /outside.json'
docker stop --time 35 "$name" > /dev/null
docker rm "$name" > /dev/null
start
grep -F ContainerSmoke "$scratch/users.html" > /dev/null
printf 'Container smoke passed: non-root runtime, tools, rejection, SQLite persistence. Evidence: %s\n' "$scratch"
