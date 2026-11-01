#!/usr/bin/env bash
# Brings up the local Noma cluster: minikube (Docker driver, containerd),
# Calico, gVisor and Postgres. The same script runs on laptops and in CI.
# Safe to re-run.
set -euo pipefail

PROFILE="${NOMA_PROFILE:-noma}"
K8S_VERSION="${NOMA_K8S_VERSION:-v1.35.1}"
CPUS="${NOMA_CPUS:-2}"
MEMORY="${NOMA_MEMORY:-4g}"
GVISOR_RELEASE="20261026.0"
GVISOR_SHA512_aarch64="da177d9bcfd9ac23899ecfedc6fd47358e9684a1f7e3f5f8ffb8bf7c0ca65f4c08730c0c503b9231b522db28b04c132a06d04d6719e0d8c04c9f82edaf03d8ae"
GVISOR_SHA512_x86_64="26e4a327565f5da0f73261a6387e2c20f4695a232e330095a3111b70efd604fdf09d734e8cc8f714dbb5c5607ce8c87ea80b26df549d83a146f2efd5a1af448a"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="${ROOT}/.dev"
mk() { minikube -p "$PROFILE" "$@"; }
kc() { kubectl --context "$PROFILE" "$@"; }

echo "==> minikube"
if ! mk status >/dev/null 2>&1; then
  mk start --driver=docker --container-runtime=containerd --cni=calico \
    --kubernetes-version="$K8S_VERSION" --cpus="$CPUS" --memory="$MEMORY"
fi

echo "==> gVisor ${GVISOR_RELEASE}"
if ! mk ssh -- test -x /usr/local/bin/runsc 2>/dev/null; then
  arch="$(mk ssh -- uname -m | tr -d '\r')"
  sum_var="GVISOR_SHA512_${arch}"
  dir="${CACHE}/gvisor-${GVISOR_RELEASE}-${arch}"
  mkdir -p "$dir"
  if [ ! -f "$dir/gvisor.tar.bz2" ]; then
    curl -fsSL -o "$dir/gvisor.tar.bz2.part" \
      "https://storage.googleapis.com/gvisor/releases/release/${GVISOR_RELEASE}/${arch}/gvisor.tar.bz2"
    mv "$dir/gvisor.tar.bz2.part" "$dir/gvisor.tar.bz2"
  fi
  echo "${!sum_var}  $dir/gvisor.tar.bz2" | shasum -a 512 -c - >/dev/null
  tar -xjf "$dir/gvisor.tar.bz2" -C "$dir"
  mk cp "$dir/runsc" /usr/local/bin/runsc
  mk cp "$dir/containerd-shim-runsc-v1" /usr/local/bin/containerd-shim-runsc-v1
  for f in "$dir"/gvisor-bin/*; do
    mk cp "$f" "/usr/local/bin/gvisor-bin/$(basename "$f")"
  done
  # gVisor's default platform is systrap, which needs no KVM.
  mk ssh -- "sudo chmod 755 /usr/local/bin/runsc /usr/local/bin/containerd-shim-runsc-v1 /usr/local/bin/gvisor-bin/* &&
    printf '\n[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runsc]\n  runtime_type = \"io.containerd.runsc.v1\"\n' | sudo tee -a /etc/containerd/config.toml >/dev/null &&
    sudo systemctl restart containerd"
fi
kc apply -f "${ROOT}/deploy/dev/runtimeclass.yaml"

echo "==> Postgres"
kc apply -f "${ROOT}/deploy/dev/namespace.yaml"
if ! kc -n noma-system get secret postgres >/dev/null 2>&1; then
  kc -n noma-system create secret generic postgres \
    --from-literal=password="$(openssl rand -hex 24)"
fi
kc apply -f "${ROOT}/deploy/dev/postgres.yaml"
kc -n noma-system rollout status statefulset/postgres --timeout=180s

echo "==> noma-server"
if ! kc -n noma-system get secret noma-server >/dev/null 2>&1; then
  pg_password="$(kc -n noma-system get secret postgres -o jsonpath='{.data.password}' | base64 -d)"
  kc -n noma-system create secret generic noma-server \
    --from-literal=database-url="postgres://noma:${pg_password}@postgres.noma-system:5432/noma?sslmode=disable" \
    --from-literal=admin-token="$(openssl rand -hex 32)"
fi
"${ROOT}/hack/deploy.sh"

echo "==> CLI config"
config="${NOMA_CONFIG:-${HOME}/.config/noma/config.json}"
mkdir -p "$(dirname "$config")"
token="$(kc -n noma-system get secret noma-server -o jsonpath='{.data.admin-token}' | base64 -d)"
(umask 077 && cat >"$config" <<JSON
{
  "token": "${token}",
  "portForward": {"context": "${PROFILE}", "namespace": "noma-system", "service": "noma-server", "port": 8080}
}
JSON
)
echo "wrote ${config}"

echo "==> ready (kubectl context: ${PROFILE})"
