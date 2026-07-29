#!/usr/bin/env sh
set -eu

helm_bin=${HELM:-helm}
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
chart_dir="$repo_root/contrib/helm/calert"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

assert_contains() {
    file=$1
    expected=$2

    if ! grep -F -- "$expected" "$file" >/dev/null; then
        printf 'expected %s in %s\n' "$expected" "$file" >&2
        exit 1
    fi
}

render() {
    name=$1
    values_file=${2:-}
    output="$tmp_dir/rendered-$name.yaml"

    if [ -n "$values_file" ]; then
        "$helm_bin" template "$name" "$chart_dir" -f "$values_file" >"$output"
    else
        "$helm_bin" template "$name" "$chart_dir" >"$output"
    fi
}

manifest_exists() {
    file=$1
    expected_kind=$2
    expected_name=$3

    awk -v expected_kind="$expected_kind" -v expected_name="$expected_name" '
        $0 == "---" { kind = ""; in_metadata = 0 }
        $1 == "kind:" { kind = $2; in_metadata = 0 }
        kind == expected_kind && $1 == "metadata:" { in_metadata = 1; next }
        in_metadata && $1 == "name:" {
            if ($2 == expected_name) {
                found = 1
            }
            in_metadata = 0
        }
        END { exit !found }
    ' "$file"
}

assert_manifest_exists() {
    if ! manifest_exists "$@"; then
        printf 'expected %s named %s in %s\n' "$2" "$3" "$1" >&2
        exit 1
    fi
}

assert_manifest_absent() {
    if manifest_exists "$@"; then
        printf 'did not expect %s named %s in %s\n' "$2" "$3" "$1" >&2
        exit 1
    fi
}

expect_lint_failure() {
    values_file=$1

    if "$helm_bin" lint "$chart_dir" -f "$values_file" >/dev/null 2>&1; then
        printf 'expected helm lint to reject %s\n' "$values_file" >&2
        exit 1
    fi
}

assert_template_volume() {
    file=$1
    release=$2
    filename=$3

    assert_contains "$file" "name: ${release}-calert-templates"
    assert_contains "$file" "- name: templates"
    assert_contains "$file" "configMap:"
    assert_contains "$file" "items:"
    assert_contains "$file" "mountPath: \"/app/static/${filename}\""
    assert_contains "$file" "- key: \"${filename}\""
    assert_contains "$file" "path: \"${filename}\""
    assert_contains "$file" "checksum/templates:"
}

cat >"$tmp_dir/generated-secret.yaml" <<'EOF'
config:
  asSecret: true
EOF

cat >"$tmp_dir/existing-secret.yaml" <<'EOF'
config:
  asSecret: true
  existingSecret:
    name: "123"
    key: "123"
EOF

cat >"$tmp_dir/generated-secret-template.yaml" <<'EOF'
config:
  asSecret: true
templates:
  message.tmpl: custom template
EOF

cat >"$tmp_dir/existing-secret-template.yaml" <<'EOF'
config:
  asSecret: true
  existingSecret:
    name: "123"
    key: "123"
templates:
  "123": custom template
EOF

cat >"$tmp_dir/env-from.yaml" <<'EOF'
envFrom:
  - prefix: SECRET_
    secretRef:
      name: calert-secrets
      optional: false
  - configMapRef:
      name: calert-env
      optional: true
EOF

cat >"$tmp_dir/invalid-env-from-both.yaml" <<'EOF'
envFrom:
  - secretRef:
      name: calert-secrets
    configMapRef:
      name: calert-env
EOF

cat >"$tmp_dir/invalid-env-from-empty.yaml" <<'EOF'
envFrom:
  - {}
EOF

cat >"$tmp_dir/invalid-env-from-unknown.yaml" <<'EOF'
envFrom:
  - secretRef:
      name: calert-secrets
    unsupported: true
EOF

cat >"$tmp_dir/group-redis.yaml" <<'EOF'
providers:
  group_alerts:
    endpoint: https://chat.example.test/webhook
    threading_mode: group
    dedup_window: 30s
    max_alerts_per_message: 5
    redis:
      address: redis:6379
      password: redis-password
      db: 2
      key_prefix: "calert:"
EOF

"$helm_bin" lint "$chart_dir"
expect_lint_failure "$tmp_dir/invalid-env-from-both.yaml"
expect_lint_failure "$tmp_dir/invalid-env-from-empty.yaml"
expect_lint_failure "$tmp_dir/invalid-env-from-unknown.yaml"

render default
assert_manifest_exists "$tmp_dir/rendered-default.yaml" ConfigMap default-calert-config
assert_manifest_absent "$tmp_dir/rendered-default.yaml" Secret default-calert-config
assert_contains "$tmp_dir/rendered-default.yaml" "- name: config"
assert_contains "$tmp_dir/rendered-default.yaml" "configMap:"
assert_contains "$tmp_dir/rendered-default.yaml" "items:"
assert_contains "$tmp_dir/rendered-default.yaml" "- key: \"config.toml\""
assert_contains "$tmp_dir/rendered-default.yaml" "path: \"config.toml\""

render generated-secret "$tmp_dir/generated-secret.yaml"
assert_manifest_exists "$tmp_dir/rendered-generated-secret.yaml" Secret generated-secret-calert-config
assert_manifest_absent "$tmp_dir/rendered-generated-secret.yaml" ConfigMap generated-secret-calert-config
assert_contains "$tmp_dir/rendered-generated-secret.yaml" "secretName: \"generated-secret-calert-config\""
assert_contains "$tmp_dir/rendered-generated-secret.yaml" "secret:"
assert_contains "$tmp_dir/rendered-generated-secret.yaml" "items:"
assert_contains "$tmp_dir/rendered-generated-secret.yaml" "- key: \"config.toml\""
assert_contains "$tmp_dir/rendered-generated-secret.yaml" "path: \"config.toml\""

render existing-secret "$tmp_dir/existing-secret.yaml"
assert_manifest_absent "$tmp_dir/rendered-existing-secret.yaml" Secret 123
assert_manifest_absent "$tmp_dir/rendered-existing-secret.yaml" ConfigMap existing-secret-calert-config
assert_contains "$tmp_dir/rendered-existing-secret.yaml" "secretName: \"123\""
assert_contains "$tmp_dir/rendered-existing-secret.yaml" "secret:"
assert_contains "$tmp_dir/rendered-existing-secret.yaml" "items:"
assert_contains "$tmp_dir/rendered-existing-secret.yaml" "- key: \"123\""
assert_contains "$tmp_dir/rendered-existing-secret.yaml" "path: \"config.toml\""

render generated-template "$tmp_dir/generated-secret-template.yaml"
assert_manifest_exists "$tmp_dir/rendered-generated-template.yaml" Secret generated-template-calert-config
assert_manifest_absent "$tmp_dir/rendered-generated-template.yaml" ConfigMap generated-template-calert-config
assert_manifest_exists "$tmp_dir/rendered-generated-template.yaml" ConfigMap generated-template-calert-templates
assert_template_volume "$tmp_dir/rendered-generated-template.yaml" generated-template message.tmpl
assert_contains "$tmp_dir/rendered-generated-template.yaml" "secretName: \"generated-template-calert-config\""
assert_contains "$tmp_dir/rendered-generated-template.yaml" "- key: \"config.toml\""

render existing-template "$tmp_dir/existing-secret-template.yaml"
assert_manifest_absent "$tmp_dir/rendered-existing-template.yaml" Secret 123
assert_manifest_absent "$tmp_dir/rendered-existing-template.yaml" ConfigMap existing-template-calert-config
assert_manifest_exists "$tmp_dir/rendered-existing-template.yaml" ConfigMap existing-template-calert-templates
assert_template_volume "$tmp_dir/rendered-existing-template.yaml" existing-template 123
assert_contains "$tmp_dir/rendered-existing-template.yaml" "secretName: \"123\""
assert_contains "$tmp_dir/rendered-existing-template.yaml" "- key: \"123\""

render env-from "$tmp_dir/env-from.yaml"
assert_contains "$tmp_dir/rendered-env-from.yaml" "envFrom:"
assert_contains "$tmp_dir/rendered-env-from.yaml" "prefix: SECRET_"
assert_contains "$tmp_dir/rendered-env-from.yaml" "secretRef:"
assert_contains "$tmp_dir/rendered-env-from.yaml" "name: calert-secrets"
assert_contains "$tmp_dir/rendered-env-from.yaml" "optional: false"
assert_contains "$tmp_dir/rendered-env-from.yaml" "configMapRef:"
assert_contains "$tmp_dir/rendered-env-from.yaml" "name: calert-env"
assert_contains "$tmp_dir/rendered-env-from.yaml" "optional: true"

render group-redis "$tmp_dir/group-redis.yaml"
assert_contains "$tmp_dir/rendered-group-redis.yaml" "threading_mode = \"group\""
assert_contains "$tmp_dir/rendered-group-redis.yaml" "dedup_window = \"30s\""
assert_contains "$tmp_dir/rendered-group-redis.yaml" "max_alerts_per_message = 5"
assert_contains "$tmp_dir/rendered-group-redis.yaml" "[providers.group_alerts.redis]"
assert_contains "$tmp_dir/rendered-group-redis.yaml" "address = \"redis:6379\""
assert_contains "$tmp_dir/rendered-group-redis.yaml" "password = \"redis-password\""
assert_contains "$tmp_dir/rendered-group-redis.yaml" "db = 2"
assert_contains "$tmp_dir/rendered-group-redis.yaml" "key_prefix = \"calert:\""

printf '%s\n' 'helm smoke matrix: passed'
