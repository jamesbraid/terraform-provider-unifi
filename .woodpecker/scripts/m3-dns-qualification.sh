#!/usr/bin/env bash
set -euo pipefail

readonly script_directory=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=m3-evidence-lib.sh
source "${script_directory}/m3-evidence-lib.sh"

readonly source_commit=${CI_COMMIT_SHA:-$(git rev-parse HEAD)}
readonly candidate_provider_binary=${M3_CANDIDATE_PROVIDER_BINARY:-}
readonly old_provider_commit=eaf41ff7dbf39c01690eaca54e99f7f9cec867b9
readonly provider_version=0.101.2
readonly old_provider_version=0.41.11
readonly provider_archive=terraform-provider-unifi_0.101.2_linux_amd64.zip
readonly provider_archive_sha256=9954f29512f0049f0e86908e2fc11a8a0e3cb11f980f3468d5e3da4005fab4b8
readonly provider_url=https://github.com/jamesbraid/terraform-provider-unifi/releases/download/v0.101.2/${provider_archive}
readonly terraform_archive=terraform_1.15.8_linux_amd64.zip
readonly terraform_archive_sha256=d25ce7b6902013ad905db3d2eab0be4cd905887fe88b81a6171b8d5503c31f3d
readonly terraform_url=https://releases.hashicorp.com/terraform/1.15.8/${terraform_archive}
readonly tofu_archive=tofu_1.12.1_linux_amd64.zip
readonly tofu_archive_sha256=1fc9af962e3632b7cd0ba27076cd9f1ced177567defe9e331ac37f5a40468575
readonly tofu_url=https://github.com/opentofu/opentofu/releases/download/v1.12.1/${tofu_archive}
readonly go_image=golang:1.25.8-bookworm@sha256:4557cf171e3cdf5053a298d5171b1a5f5734d920260c25f22c79e94760eb2084
readonly controller_index_sha256=sha256:584be3a2e45c4913e1bc373eff9c7330609c82085d4fc6f5ea365abdcdb3e664
readonly controller_manifest_sha256=sha256:9d19c8d03948a77d28181743fb81a515aa51a5cea0856bc0491b34d92a01bcf4
readonly controller_image=ghcr.io/jamesbraid/unifi-network@${controller_manifest_sha256}

readonly run_id=${CI_PIPELINE_NUMBER:?CI_PIPELINE_NUMBER is required}
readonly prefix=provider-m3q-${run_id}
readonly network=${prefix}-network
readonly controller=${prefix}-controller
readonly tools_volume=${prefix}-tools
readonly config_volume=${prefix}-config
readonly terraform_state_volume=${prefix}-terraform-state
readonly tofu_state_volume=${prefix}-tofu-state
readonly terraform_upgrade_state_volume=${prefix}-terraform-upgrade-state
readonly tofu_upgrade_state_volume=${prefix}-tofu-upgrade-state
readonly terraform_legacy_state_volume=${prefix}-terraform-legacy-state
readonly tofu_legacy_state_volume=${prefix}-tofu-legacy-state
readonly source_volume=${prefix}-source
readonly go_cache_volume=${prefix}-go-cache
readonly old_source_volume=${prefix}-old-source
readonly old_go_cache_volume=${prefix}-old-go-cache
readonly go_unifi_proxy_root=${GO_UNIFI_PROXY_ROOT:-/tmp/go-unifi-proxy}
work_root=$(mktemp -d "${TMPDIR:-/tmp}/provider-m3q.XXXXXX")
readonly work_root
readonly dns_name=m0-dns.example.invalid

cleanup() {
    local result=$1
    trap - EXIT
    docker rm --force "${controller}" >/dev/null 2>&1 || true
    docker network rm "${network}" >/dev/null 2>&1 || true
    docker volume rm "${tools_volume}" "${config_volume}" \
        "${terraform_state_volume}" "${tofu_state_volume}" \
        "${terraform_upgrade_state_volume}" "${tofu_upgrade_state_volume}" \
        "${terraform_legacy_state_volume}" "${tofu_legacy_state_volume}" \
        "${source_volume}" "${go_cache_volume}" \
        "${old_source_volume}" "${old_go_cache_volume}" >/dev/null 2>&1 || true
    exit "${result}"
}
trap 'cleanup $?' EXIT

test "$(docker info --format '{{.Architecture}}')" = x86_64
git cat-file -e "${source_commit}^{commit}"
git cat-file -e "${old_provider_commit}^{commit}"

download() {
    local url=$1
    local sha256=$2
    local destination=$3

    curl --fail --location --silent --show-error --output "${destination}" "${url}"
    printf '%s  %s\n' "${sha256}" "${destination}" | sha256sum --check -
}

download "${provider_url}" "${provider_archive_sha256}" "${work_root}/${provider_archive}"
download "${terraform_url}" "${terraform_archive_sha256}" "${work_root}/${terraform_archive}"
download "${tofu_url}" "${tofu_archive_sha256}" "${work_root}/${tofu_archive}"

mkdir -p "${work_root}/tools/provider" "${work_root}/tools/provider-legacy" \
    "${work_root}/tools/provider-old" \
    "${work_root}/config"
unzip -q "${work_root}/${provider_archive}" -d "${work_root}/tools/provider-legacy"
unzip -q "${work_root}/${terraform_archive}" -d "${work_root}/tools"
unzip -q "${work_root}/${tofu_archive}" -d "${work_root}/tools"
chmod +x "${work_root}/tools/terraform" "${work_root}/tools/tofu" \
    "${work_root}/tools/provider-legacy/terraform-provider-unifi_v${provider_version}"
if [[ -n ${candidate_provider_binary} ]]; then
    install_prebuilt_candidate \
        "${candidate_provider_binary}" \
        "${work_root}/tools/provider/terraform-provider-unifi_v${provider_version}"
fi
cat >"${work_root}/tools/cli.tfrc" <<'EOF'
provider_installation {
  dev_overrides {
    "registry.terraform.io/ubiquiti-community/unifi" = "/tools/provider"
  }
  direct {}
}
EOF
cat >"${work_root}/tools/cli-legacy.tfrc" <<'EOF'
provider_installation {
  dev_overrides {
    "registry.terraform.io/ubiquiti-community/unifi" = "/tools/provider-legacy"
  }
  direct {}
}
EOF
cat >"${work_root}/tools/cli-old.tfrc" <<'EOF'
provider_installation {
  dev_overrides {
    "registry.terraform.io/ubiquiti-community/unifi" = "/tools/provider-old"
  }
  direct {}
}
EOF

cp -R .woodpecker/fixtures/m3/. "${work_root}/config/"
for volume in "${tools_volume}" "${config_volume}" "${terraform_state_volume}" \
    "${tofu_state_volume}" "${terraform_upgrade_state_volume}" \
    "${tofu_upgrade_state_volume}" "${terraform_legacy_state_volume}" \
    "${tofu_legacy_state_volume}" "${source_volume}" "${go_cache_volume}" \
    "${old_source_volume}" \
    "${old_go_cache_volume}"; do
    docker volume create "${volume}" >/dev/null
done

populate_volume() {
    local directory=$1
    local volume=$2

    tar -C "${directory}" -cf - . | docker run --rm --interactive \
        --entrypoint /bin/sh --mount "type=volume,src=${volume},dst=/target" \
        "${go_image}" -c 'tar -xf - -C /target'
}

populate_volume "${work_root}/tools" "${tools_volume}"
populate_volume "${work_root}/config" "${config_volume}"
if [[ -z ${candidate_provider_binary} ]]; then
    # The candidate depends on the unreleased go-unifi module.  The workflow
    # bootstraps that module into a file GOPROXY on the host, but this build
    # runs inside a separate Docker container.  Keep the acquisition boundary
    # explicit by mounting the retained proxy read-only; otherwise Go falls
    # through to a direct GitHub lookup for v1.102.0, which is both fragile and
    # unavailable on the isolated build network.
    if [[ ! -d ${go_unifi_proxy_root} ]]; then
        echo "go-unifi proxy root ${go_unifi_proxy_root} is missing:" \
            "bootstrap it on this host with bootstrap-go-unifi-proxy.sh or" \
            "pass a prebuilt candidate via M3_CANDIDATE_PROVIDER_BINARY" >&2
        exit 1
    fi
    git archive "${source_commit}" | docker run --rm --interactive \
        --entrypoint /bin/sh \
        --mount "type=volume,src=${source_volume},dst=/source" \
        "${go_image}" -c 'tar -xf - -C /source'
    docker run --rm --platform linux/amd64 \
        --env CGO_ENABLED=0 \
        --env GOCACHE=/go/build-cache --env GOMODCACHE=/go/module-cache \
        --env GOTELEMETRY=off --env GOTOOLCHAIN=local \
        --env GOPROXY=file:///go-unifi-proxy,https://proxy.golang.org \
        --env GOSUMDB=off --env GOVCS=*:off --env GIT_TERMINAL_PROMPT=0 \
        --mount "type=volume,src=${source_volume},dst=/source,readonly" \
        --mount "type=volume,src=${go_cache_volume},dst=/go" \
        --mount "type=volume,src=${tools_volume},dst=/tools" \
        --mount "type=bind,src=${go_unifi_proxy_root},dst=/go-unifi-proxy,readonly" \
        --workdir /source "${go_image}" \
        go build -trimpath -buildvcs=false \
        -o "/tools/provider/terraform-provider-unifi_v${provider_version}" .
fi
git archive "${old_provider_commit}" | docker run --rm --interactive \
    --entrypoint /bin/sh \
    --mount "type=volume,src=${old_source_volume},dst=/source" \
    "${go_image}" -c 'tar -xf - -C /source'
docker run --rm --platform linux/amd64 \
    --env GOCACHE=/go/build-cache --env GOMODCACHE=/go/module-cache \
    --env GOTELEMETRY=off --env GOTOOLCHAIN=local \
    --mount "type=volume,src=${old_source_volume},dst=/source,readonly" \
    --mount "type=volume,src=${old_go_cache_volume},dst=/go" \
    --mount "type=volume,src=${tools_volume},dst=/tools" \
    --workdir /source "${go_image}" \
    go build -trimpath -o "/tools/provider-old/terraform-provider-unifi_v${old_provider_version}" .
old_provider_binary_sha256=$(docker run --rm --entrypoint sha256sum \
    --mount "type=volume,src=${tools_volume},dst=/tools,readonly" \
    "${go_image}" "/tools/provider-old/terraform-provider-unifi_v${old_provider_version}" \
    | awk '{print $1}')
docker network create "${network}" >/dev/null
docker pull --platform linux/amd64 "${controller_image}"
controller_config_sha256=$(docker image inspect --format '{{.Id}}' "${controller_image}")
provider_binary_sha256=$(docker run --rm --entrypoint sha256sum \
    --mount "type=volume,src=${tools_volume},dst=/tools,readonly" \
    "${go_image}" "/tools/provider/terraform-provider-unifi_v${provider_version}" \
    | awk '{print $1}')
legacy_provider_binary_sha256=$(sha256sum "${work_root}/tools/provider-legacy/terraform-provider-unifi_v${provider_version}" | awk '{print $1}')
terraform_binary_sha256=$(sha256sum "${work_root}/tools/terraform" | awk '{print $1}')
tofu_binary_sha256=$(sha256sum "${work_root}/tools/tofu" | awk '{print $1}')

wait_healthy() {
    for _ in $(seq 1 240); do
        status=$(docker inspect --format '{{.State.Health.Status}}' "${controller}")
        if [[ ${status} = healthy ]]; then
            return
        fi
        if [[ $(docker inspect --format '{{.State.Running}}' "${controller}") != true ]]; then
            docker inspect --format '{{json .State}}' "${controller}" >&2
            docker logs "${controller}" >&2
            return 1
        fi
        sleep 5
    done
    docker inspect --format '{{json .State.Health}}' "${controller}" >&2
    docker logs "${controller}" >&2
    return 1
}

start_controller() {
    docker run --detach --name "${controller}" --network "${network}" \
        --network-alias controller --init --platform linux/amd64 \
        "${controller_image}" >/dev/null
    wait_healthy
}

stop_controller() {
    docker rm --force "${controller}" >/dev/null
    if docker inspect "${controller}" >/dev/null 2>&1; then
        return 1
    fi
}

run_cli_config() {
    local cli=$1
    local state_volume=$2
    local fixture=$3
    local cli_config=$4
    shift 4

    docker run --rm --platform linux/amd64 --network "${network}" \
        --tmpfs /tmp:rw,exec,nosuid,size=1g \
        --env CHECKPOINT_DISABLE=1 --env TF_IN_AUTOMATION=1 \
        --env "TF_CLI_CONFIG_FILE=/tools/${cli_config}" --env TF_DATA_DIR=/tmp/tfdata \
        --env UNIFI_USERNAME=admin --env UNIFI_PASSWORD=admin \
        --env UNIFI_INSECURE=true --env UNIFI_API=https://controller:8443 \
        --mount "type=volume,src=${tools_volume},dst=/tools,readonly" \
        --mount "type=volume,src=${config_volume},dst=/config,readonly" \
        --mount "type=volume,src=${state_volume},dst=/state" \
        --workdir "/config/${fixture}" "${go_image}" "/tools/${cli}" "$@"
}

run_cli() {
    local cli=$1
    local state_volume=$2
    local fixture=$3
    shift 3

    run_cli_config "${cli}" "${state_volume}" "${fixture}" cli.tfrc "$@"
}

expect_changes() {
    local cli=$1
    local state_volume=$2
    local fixture=$3
    local plan=$4
    local cli_config=$5

    set +e
    run_cli_config "${cli}" "${state_volume}" "${fixture}" "${cli_config}" plan -input=false \
        -detailed-exitcode -state=/state/state.tfstate -out="/state/${plan}" \
        -var "dns_name=${dns_name}"
    result=$?
    set -e
    test "${result}" -eq 2
}

qualify_cli() {
    local cli=$1
    local state_volume=$2
    local cli_config=$3
    local cross_config=$4
    local label=$5

    start_controller
    run_cli_config "${cli}" "${state_volume}" initial "${cli_config}" apply -auto-approve -input=false \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"
    expect_changes "${cli}" "${state_volume}" updated update.tfplan "${cli_config}"
    run_cli_config "${cli}" "${state_volume}" updated "${cli_config}" apply -auto-approve -input=false \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli_config "${cli}" "${state_volume}" updated "${cli_config}" plan -input=false -detailed-exitcode \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"

    expect_changes "${cli}" "${state_volume}" replacement replacement.tfplan "${cli_config}"
    run_cli_config "${cli}" "${state_volume}" replacement "${cli_config}" show -json /state/replacement.tfplan \
        >"${work_root}/${label}-replacement-plan.json"
    jq -e '[.resource_changes[] | select(.address == "unifi_dns_record.test").change.actions]
        | length == 1 and (.[0] | index("create") != null and index("delete") != null)' \
        "${work_root}/${label}-replacement-plan.json" >/dev/null

    docker restart "${controller}" >/dev/null
    wait_healthy
    run_cli_config "${cli}" "${state_volume}" updated "${cli_config}" apply -refresh-only -auto-approve -input=false \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli_config "${cli}" "${state_volume}" updated "${cli_config}" plan -input=false -detailed-exitcode \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"

    record_id=$(run_cli_config "${cli}" "${state_volume}" updated "${cli_config}" output \
        -state=/state/state.tfstate -raw dns_record_id)
    test -n "${record_id}"
    run_cli_config "${cli}" "${state_volume}" updated "${cli_config}" state rm \
        -state=/state/state.tfstate unifi_dns_record.test
    run_cli_config "${cli}" "${state_volume}" imported "${cli_config}" import -input=false \
        -state=/state/state.tfstate -var "dns_name=${dns_name}" \
        unifi_dns_record.test "${record_id}"
    run_cli_config "${cli}" "${state_volume}" imported "${cli_config}" plan -input=false -detailed-exitcode \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli_config "${cli}" "${state_volume}" imported "${cross_config}" plan -input=false -detailed-exitcode \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli_config "${cli}" "${state_volume}" imported "${cli_config}" plan -input=false -detailed-exitcode \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli_config "${cli}" "${state_volume}" imported "${cli_config}" show -json /state/state.tfstate \
        >"${work_root}/${label}-state.json"
    jq --sort-keys \
        '.values.root_module.resources[] | select(.address == "unifi_dns_record.test") | .values | del(.id)' \
        "${work_root}/${label}-state.json" >"${work_root}/${label}-state-normalized.json"

    run_cli_config "${cli}" "${state_volume}" imported "${cli_config}" destroy -auto-approve -input=false \
        -state=/state/state.tfstate -var "dns_name=${dns_name}"
    stop_controller
}

qualify_state_upgrade() {
    local cli=$1
    local state_volume=$2

    start_controller
    run_cli_config "${cli}" "${state_volume}" upgrade-old cli-old.tfrc \
        apply -auto-approve -input=false -state=/state/state.tfstate \
        -var "dns_name=${dns_name}"
    run_cli_config "${cli}" "${state_volume}" upgrade-old cli-old.tfrc \
        show -json /state/state.tfstate >"${work_root}/${cli}-upgrade-v0-state.json"
    jq -e '.values.root_module.resources[]
        | select(.address == "unifi_dns_record.test")
        | .schema_version == 0 and .values.ttl == 300' \
        "${work_root}/${cli}-upgrade-v0-state.json" >/dev/null

    run_cli "${cli}" "${state_volume}" upgrade-current apply -auto-approve \
        -input=false -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli "${cli}" "${state_volume}" upgrade-current plan -input=false \
        -detailed-exitcode -state=/state/state.tfstate -var "dns_name=${dns_name}"
    run_cli "${cli}" "${state_volume}" upgrade-current show -json \
        /state/state.tfstate >"${work_root}/${cli}-upgrade-v1-state.json"
    jq -e '.values.root_module.resources[]
        | select(.address == "unifi_dns_record.test")
        | .schema_version == 1 and .values.ttl == "5m0s"' \
        "${work_root}/${cli}-upgrade-v1-state.json" >/dev/null
    run_cli "${cli}" "${state_volume}" upgrade-current destroy -auto-approve \
        -input=false -state=/state/state.tfstate -var "dns_name=${dns_name}"
    stop_controller
}

qualify_cli terraform "${terraform_state_volume}" cli.tfrc cli-legacy.tfrc terraform-current
qualify_cli tofu "${tofu_state_volume}" cli.tfrc cli-legacy.tfrc tofu-current
qualify_cli terraform "${terraform_legacy_state_volume}" cli-legacy.tfrc cli.tfrc terraform-legacy
qualify_cli tofu "${tofu_legacy_state_volume}" cli-legacy.tfrc cli.tfrc tofu-legacy
qualify_state_upgrade terraform "${terraform_upgrade_state_volume}"
qualify_state_upgrade tofu "${tofu_upgrade_state_volume}"
cmp "${work_root}/terraform-current-state-normalized.json" "${work_root}/tofu-current-state-normalized.json"
cmp "${work_root}/terraform-current-state-normalized.json" "${work_root}/terraform-legacy-state-normalized.json"
cmp "${work_root}/terraform-current-state-normalized.json" "${work_root}/tofu-legacy-state-normalized.json"
normalized_state_sha256=$(sha256sum "${work_root}/terraform-current-state-normalized.json" | awk '{print $1}')

docker network rm "${network}" >/dev/null
if docker network inspect "${network}" >/dev/null 2>&1; then
    exit 1
fi

receipt=$(jq --compact-output --null-input \
    --arg source_commit "${source_commit}" \
    --arg old_provider_commit "${old_provider_commit}" \
    --arg old_provider_binary_sha256 "${old_provider_binary_sha256}" \
    --arg provider_archive_sha256 "${provider_archive_sha256}" \
    --arg provider_binary_sha256 "${provider_binary_sha256}" \
    --arg legacy_provider_binary_sha256 "${legacy_provider_binary_sha256}" \
    --arg terraform_archive_sha256 "${terraform_archive_sha256}" \
    --arg terraform_binary_sha256 "${terraform_binary_sha256}" \
    --arg tofu_archive_sha256 "${tofu_archive_sha256}" \
    --arg tofu_binary_sha256 "${tofu_binary_sha256}" \
    --arg controller_index_sha256 "${controller_index_sha256}" \
    --arg controller_manifest_sha256 "${controller_manifest_sha256}" \
    --arg controller_config_sha256 "${controller_config_sha256}" \
    --arg normalized_state_sha256 "${normalized_state_sha256}" \
    '{format_version: 1, gate: "M3 DNS managed-operation qualification", result: "pass", source_commit: $source_commit, platform: "linux/amd64", provider_version: "0.101.2", provider_binary_sha256: $provider_binary_sha256, legacy_provider: {version: "0.101.2", archive_sha256: $provider_archive_sha256, binary_sha256: $legacy_provider_binary_sha256}, state_upgrade_source: {version: "0.41.11", commit: $old_provider_commit, binary_sha256: $old_provider_binary_sha256}, terraform: {version: "1.15.8", archive_sha256: $terraform_archive_sha256, binary_sha256: $terraform_binary_sha256}, tofu: {version: "1.12.1", archive_sha256: $tofu_archive_sha256, binary_sha256: $tofu_binary_sha256}, target: {product: "UniFi Network", version: "10.4.57", index_sha256: $controller_index_sha256, platform_manifest_sha256: $controller_manifest_sha256, config_sha256: $controller_config_sha256}, lifecycle: {fresh_target_per_cli_and_adapter: true, create: true, update: true, omitted_optional_fields: true, configured_optional_fields: true, replacement_plan: true, restart_refresh: true, import: true, v0_integer_ttl_state_upgrade: true, no_op_plan: true, delete: true, cleanup: true, bidirectional_adapter_state_round_trip: true}, normalized_state_sha256: $normalized_state_sha256, cli_outcomes_equivalent: true, adapter_outcomes_equivalent: true}')
if [[ -n ${M3_LIFECYCLE_RECEIPT_OUTPUT:-} ]]; then
    printf '%s\n' "${receipt}" >"${M3_LIFECYCLE_RECEIPT_OUTPUT}"
fi
printf 'M3_DNS_RECEIPT=%s\n' "${receipt}"
