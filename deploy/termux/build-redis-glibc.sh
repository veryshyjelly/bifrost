#!/bin/bash
# Run inside bifrost-debian. Keep the tested libc allocator on this Android kernel.
set -euo pipefail
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export DEBIAN_FRONTEND=noninteractive
umask 077
apt-get install -y --no-install-recommends build-essential curl ca-certificates
source_dir=$(mktemp -d /tmp/bifrost-redis-build.XXXXXX)
trap 'rm -rf "$source_dir"' EXIT
cd "$source_dir"
curl --fail --location --retry 2 --connect-timeout 15 --max-time 180 \
  --output redis-8.10.3.tar.gz https://download.redis.io/releases/redis-8.10.3.tar.gz
printf '%s\n' 'edde778f6bdee7581e42fc139ca9e07e2822212092f7501f99989656e3629d02  redis-8.10.3.tar.gz' | sha256sum --check
tar -xzf redis-8.10.3.tar.gz
make -C redis-8.10.3/src -j2 MALLOC=libc OPTIMIZATION=-O2 \
  redis-server redis-cli redis-check-rdb redis-check-aof
target_dir=/opt/bifrost-redis-glibc/8.10.3/bin
test ! -e "$target_dir"
mkdir -p "$target_dir"
for executable in redis-server redis-cli redis-check-rdb redis-check-aof; do
  install -m 755 "redis-8.10.3/src/$executable" "$target_dir/$executable"
done
"$target_dir/redis-server" --version
sha256sum "$target_dir/"*
