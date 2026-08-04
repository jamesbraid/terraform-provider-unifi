#!/usr/bin/env bash
set -euo pipefail

readonly source_commit=${CI_COMMIT_SHA:-$(git rev-parse HEAD)}
readonly go_image='golang:1.25.8-bookworm@sha256:4557cf171e3cdf5053a298d5171b1a5f5734d920260c25f22c79e94760eb2084'
readonly terraform_image='hashicorp/terraform@sha256:7ae513256f7ce67879e218ae8593d6fbe216ec9e123abe6c94e4e10704857963'
readonly tofu_image='ghcr.io/opentofu/opentofu@sha256:1cdf44e7a44a67c7838ada83c71ad28b4c4e1571b47ad1c5b592624c2c8f35b5'
readonly uos_image='ghcr.io/jamesbraid/unifi-os-server@sha256:dc7e093b65c4ec27af1bba8b7e7639bb53805266c9fdbcb869c03e94bab7c2cf'
readonly run_id=${UOS_RUN_ID:-local}
if [[ ! ${run_id} =~ ^[a-zA-Z0-9_.-]+$ ]]; then
    echo "invalid UOS_RUN_ID" >&2
    exit 2
fi

readonly prefix=provider-m0-uos-${run_id}
readonly network=${prefix}-network
readonly tools_volume=${prefix}-tools
readonly source_volume=${prefix}-source
readonly go_cache_volume=${prefix}-go-cache
readonly config_volume=${prefix}-config
readonly terraform_state_volume=${prefix}-terraform-state
readonly tofu_state_volume=${prefix}-tofu-state
readonly terraform_uos_volume=${prefix}-terraform-uos
readonly tofu_uos_volume=${prefix}-tofu-uos
work_root=$(mktemp -d "${TMPDIR:-/tmp}/provider-m0-uos.XXXXXX")
readonly work_root

cleanup() {
    local result=$1
    trap - EXIT
    docker rm --force "${prefix}-terraform" "${prefix}-tofu" >/dev/null 2>&1 || true
    docker network rm "${network}" >/dev/null 2>&1 || true
    docker volume rm "${tools_volume}" "${source_volume}" "${go_cache_volume}" \
        "${config_volume}" "${terraform_state_volume}" "${tofu_state_volume}" \
        "${terraform_uos_volume}" "${tofu_uos_volume}" >/dev/null 2>&1 || true
    rm -rf "${work_root}"
    exit "${result}"
}
trap 'cleanup $?' EXIT

test "$(docker info --format '{{.Architecture}}')" = aarch64
git cat-file -e "${source_commit}^{commit}"
docker network create "${network}" >/dev/null
for volume in "${tools_volume}" "${source_volume}" "${go_cache_volume}" \
    "${config_volume}" "${terraform_state_volume}" "${tofu_state_volume}" \
    "${terraform_uos_volume}" "${tofu_uos_volume}"; do
    docker volume create "${volume}" >/dev/null
done

git archive "${source_commit}" | docker run --rm --interactive \
    --entrypoint /bin/sh --mount "type=volume,src=${source_volume},dst=/source" \
    "${go_image}" -c 'tar -xf - -C /source'
tar -C .woodpecker/fixtures/m3 -cf - . | docker run --rm --interactive \
    --entrypoint /bin/sh --mount "type=volume,src=${config_volume},dst=/config" \
    "${go_image}" -c 'tar -xf - -C /config'

docker run --rm --platform linux/arm64 --entrypoint /bin/sh \
    --mount "type=volume,src=${tools_volume},dst=/tools" \
    "${terraform_image}" -c 'cp /bin/terraform /tools/terraform'
docker run --rm --platform linux/arm64 --entrypoint /bin/sh \
    --mount "type=volume,src=${tools_volume},dst=/tools" \
    "${tofu_image}" -c 'cp /usr/local/bin/tofu /tools/tofu'
docker run --rm --platform linux/arm64 \
    --env GOCACHE=/go/build-cache --env GOMODCACHE=/go/module-cache \
    --env GOTELEMETRY=off --env GOTOOLCHAIN=local \
    --mount "type=volume,src=${source_volume},dst=/source,readonly" \
    --mount "type=volume,src=${go_cache_volume},dst=/go" \
    --mount "type=volume,src=${tools_volume},dst=/tools" \
    --workdir /source "${go_image}" \
    go build -trimpath -o /tools/terraform-provider-unifi_v0.101.2 .
docker run --rm --entrypoint /bin/sh \
    --mount "type=volume,src=${tools_volume},dst=/tools" \
    "${go_image}" -c 'printf "%s\n" \
        "provider_installation {" \
        "  dev_overrides {" \
        "    \"registry.terraform.io/ubiquiti-community/unifi\" = \"/tools\"" \
        "  }" \
        "  direct {}" \
        "}" > /tools/cli.tfrc'

provider_binary_sha256=$(docker run --rm --entrypoint sha256sum \
    --mount "type=volume,src=${tools_volume},dst=/tools,readonly" \
    "${go_image}" /tools/terraform-provider-unifi_v0.101.2 | awk '{print $1}')
terraform_binary_sha256=$(docker run --rm --entrypoint sha256sum \
    --mount "type=volume,src=${tools_volume},dst=/tools,readonly" \
    "${go_image}" /tools/terraform | awk '{print $1}')
tofu_binary_sha256=$(docker run --rm --entrypoint sha256sum \
    --mount "type=volume,src=${tools_volume},dst=/tools,readonly" \
    "${go_image}" /tools/tofu | awk '{print $1}')

network_ready() {
    local container=$1
    docker exec "${container}" /bin/bash -c '
        test -s /unifi/api-key || exit 1
        response=/run/provider-m0-dns-readiness.json
        test "$(curl -ks --max-time 8 -o "${response}" -w "%{http_code}" \
            -H "X-API-KEY: $(cat /unifi/api-key)" \
            https://127.0.0.1/proxy/network/v2/api/site/default/static-dns)" = 200 &&
            jq -e '\''type == "array" or (type == "object" and (.data | type == "array"))'\'' \
                "${response}" >/dev/null 2>&1
    '
}

wait_healthy() {
    local container=$1
    for _ in $(seq 1 180); do
        status=$(docker inspect --format '{{.State.Health.Status}}' "${container}")
        if [[ ${status} = healthy ]] && network_ready "${container}"; then
            return
        fi
        if [[ ${status} = unhealthy ]]; then
            docker logs "${container}" >&2
            return 1
        fi
        sleep 5
    done
    docker logs "${container}" >&2
    return 1
}

start_uos() {
    local cli=$1
    local data_volume=$2
    local container=${prefix}-${cli}
    docker run --detach --name "${container}" --hostname "uos-${cli}" \
        --network "${network}" --network-alias "uos-${cli}" \
        --platform linux/arm64 --cgroupns host --cap-drop ALL \
        --cap-add SYS_ADMIN --cap-add NET_ADMIN --cap-add NET_RAW \
        --cap-add NET_BIND_SERVICE --cap-add DAC_OVERRIDE --cap-add DAC_READ_SEARCH \
        --cap-add FOWNER --cap-add CHOWN --cap-add SETUID --cap-add SETGID \
        --cap-add KILL --cap-add SYS_CHROOT --cap-add SYS_PTRACE \
        --cap-add SYS_RESOURCE --cap-add AUDIT_WRITE --cap-add MKNOD \
        --tmpfs /run:exec --tmpfs /run/lock --tmpfs /tmp:exec \
        --tmpfs /var/lib/journal --tmpfs /var/opt/unifi/tmp:size=64m \
        --volume /sys/fs/cgroup:/sys/fs/cgroup:rw \
        --mount "type=volume,src=${data_volume},dst=/unifi" \
        "${uos_image}" >/dev/null
    wait_healthy "${container}"
}

run_cli() {
    local cli=$1
    local state_volume=$2
    local fixture=$3
    local api_key=$4
    shift 4
    docker run --rm --platform linux/arm64 --network "${network}" \
        --tmpfs /tmp:rw,exec,nosuid,size=1g \
        --env CHECKPOINT_DISABLE=1 --env TF_IN_AUTOMATION=1 \
        --env TF_CLI_CONFIG_FILE=/tools/cli.tfrc --env TF_DATA_DIR=/tmp/tfdata \
        --env "UNIFI_API_KEY=${api_key}" --env UNIFI_INSECURE=true \
        --env "UNIFI_API=https://uos-${cli}:443" \
        --mount "type=volume,src=${tools_volume},dst=/tools,readonly" \
        --mount "type=volume,src=${config_volume},dst=/config,readonly" \
        --mount "type=volume,src=${state_volume},dst=/state" \
        --workdir "/config/${fixture}" "${go_image}" "/tools/${cli}" "$@"
}

qualify_cli() {
    local cli=$1
    local state_volume=$2
    local data_volume=$3
    local container=${prefix}-${cli}
    local dns_name=m0-uos.example.invalid
    start_uos "${cli}" "${data_volume}"
    api_key=$(docker exec "${container}" cat /unifi/api-key)
    test -n "${api_key}"

    run_cli "${cli}" "${state_volume}" initial "${api_key}" apply -auto-approve \
        -input=false -state=/state/state.tfstate -var "dns_name=${dns_name}"
    docker restart "${container}" >/dev/null
    wait_healthy "${container}"
    restarted_api_key=$(docker exec "${container}" cat /unifi/api-key)
    test "${api_key}" = "${restarted_api_key}"
    run_cli "${cli}" "${state_volume}" initial "${api_key}" apply -refresh-only \
        -auto-approve -input=false -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli "${cli}" "${state_volume}" initial "${api_key}" plan -input=false \
        -detailed-exitcode -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli "${cli}" "${state_volume}" initial "${api_key}" show -json \
        /state/state.tfstate >"${work_root}/${cli}-state.json"
    jq --sort-keys \
        '.values.root_module.resources[] | select(.address == "unifi_dns_record.test") | .values | del(.id)' \
        "${work_root}/${cli}-state.json" >"${work_root}/${cli}-state-normalized.json"
    run_cli "${cli}" "${state_volume}" initial "${api_key}" destroy -auto-approve \
        -input=false -state=/state/state.tfstate -var "dns_name=${dns_name}"
    docker rm --force "${container}" >/dev/null
    docker volume rm "${data_volume}" >/dev/null
}

qualify_cli terraform "${terraform_state_volume}" "${terraform_uos_volume}"
qualify_cli tofu "${tofu_state_volume}" "${tofu_uos_volume}"
cmp "${work_root}/terraform-state-normalized.json" "${work_root}/tofu-state-normalized.json"
normalized_state_sha256=$(sha256sum "${work_root}/terraform-state-normalized.json" | awk '{print $1}')

receipt=$(jq --compact-output --null-input \
    --arg source_commit "${source_commit}" \
    --arg provider_binary_sha256 "${provider_binary_sha256}" \
    --arg terraform_binary_sha256 "${terraform_binary_sha256}" \
    --arg tofu_binary_sha256 "${tofu_binary_sha256}" \
    --arg normalized_state_sha256 "${normalized_state_sha256}" \
    '{format_version: 1, gate: "M0 native arm64 UOS DNS restart qualification", result: "pass", source_commit: $source_commit, platform: "linux/arm64", provider: {version: "0.101.2", binary_sha256: $provider_binary_sha256}, terraform: {version: "1.15.8", image_sha256: "sha256:7ae513256f7ce67879e218ae8593d6fbe216ec9e123abe6c94e4e10704857963", binary_sha256: $terraform_binary_sha256}, tofu: {version: "1.12.1", image_sha256: "sha256:1cdf44e7a44a67c7838ada83c71ad28b4c4e1571b47ad1c5b592624c2c8f35b5", binary_sha256: $tofu_binary_sha256}, target: {product: "UniFi OS Server", version: "5.1.21", image_build: "5.1.21-2", network_version: "10.4.57", image_sha256: "sha256:dc7e093b65c4ec27af1bba8b7e7639bb53805266c9fdbcb869c03e94bab7c2cf", architecture: "arm64", runtime: "systemd", authentication: "per-volume API key"}, lifecycle: {fresh_persisted_target_per_cli: true, create: true, persisted_restart: true, api_key_persisted: true, refresh: true, no_op_plan: true, delete: true, cleanup: true}, normalized_state_sha256: $normalized_state_sha256, cli_outcomes_equivalent: true, evidence_redaction: {api_key_retained: false, raw_state_retained: false, fixtures_are_synthetic: true}}')
if [[ -n ${M0_UOS_RECEIPT_OUTPUT:-} ]]; then
    printf '%s\n' "${receipt}" >"${M0_UOS_RECEIPT_OUTPUT}"
fi
printf 'M0_UOS_RECEIPT=%s\n' "${receipt}"
