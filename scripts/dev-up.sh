#!/usr/bin/env bash
set -euo pipefail

context="kind-yacd-dev"
tilt_host="localhost"
tilt_port="10350"

repo_root="$(git rev-parse --show-toplevel)"
git_common_dir="$(git rev-parse --path-format=absolute --git-common-dir)"
state_dir="$(dirname "$git_common_dir")/.run/yacd-dev"

pid_file="$state_dir/tilt.pid"
log_file="$state_dir/tilt.log"
port_file="$state_dir/tilt.port"
worktree_file="$state_dir/worktree"
branch_file="$state_dir/branch"

die() {
  echo "error: $*" >&2
  exit 1
}

read_state_file() {
  local file="$1"
  local value=""
  if [ -f "$file" ]; then
    IFS= read -r value <"$file" || true
  fi
  printf '%s' "$value"
}

process_command() {
  local pid="$1"
  ps -p "$pid" -o command= 2>/dev/null || true
}

is_tilt_pid() {
  local pid="$1"
  local command_line

  [ -n "$pid" ] || return 1
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1

  command_line="$(process_command "$pid")"
  [[ "$command_line" == *"tilt"* ]]
}

clear_stale_state() {
  rm -f "$pid_file" "$port_file" "$worktree_file" "$branch_file"
}

print_tilt_log_tail() {
  if [ -f "$log_file" ]; then
    echo "last Tilt log lines:" >&2
    tail -n 80 "$log_file" >&2 || true
  fi
}

wait_for_tilt_http() {
  local pid="$1"
  local attempt

  for attempt in {1..60}; do
    if ! is_tilt_pid "$pid"; then
      print_tilt_log_tail
      die "Tilt exited before the dev stack became ready"
    fi

    if curl --silent --fail --max-time 1 "http://${tilt_host}:${tilt_port}/" >/dev/null 2>&1; then
      return 0
    fi

    sleep 1
  done

  print_tilt_log_tail
  die "timed out waiting for Tilt to respond on ${tilt_host}:${tilt_port}"
}

current_branch="$(git branch --show-current)"
if [ -z "$current_branch" ]; then
  current_branch="$(git rev-parse --short HEAD)"
fi

mkdir -p "$state_dir"

ctlptl apply -f dev/ctlptl.yaml
kubectl config use-context "$context" >/dev/null

existing_pid="$(read_state_file "$pid_file")"
owner_worktree="$(read_state_file "$worktree_file")"

if is_tilt_pid "$existing_pid"; then
  if [ "$owner_worktree" != "$repo_root" ]; then
    die "Tilt is already running for ${owner_worktree:-an unknown worktree}; run 'moon run root:dev-down' there before starting this worktree"
  fi

  echo "Tilt is already running for this worktree on port ${tilt_port}."
  pid="$existing_pid"
else
  if [ -n "$existing_pid" ]; then
    echo "Removing stale Tilt state for pid ${existing_pid}."
    clear_stale_state
  fi

  echo "Starting Tilt in the background. Logs: ${log_file}"
  command -v python3 >/dev/null || die "python3 is required to launch Tilt outside Moon's task process group"
  : >"$log_file"
  pid="$(
    python3 - "$repo_root" "$log_file" "$context" "$tilt_host" "$tilt_port" <<'PY'
import subprocess
import sys

repo_root, log_file, context, tilt_host, tilt_port = sys.argv[1:]
with open(log_file, "ab", buffering=0) as log:
    process = subprocess.Popen(
        [
            "tilt",
            "up",
            "--context",
            context,
            "--host",
            tilt_host,
            "--port",
            tilt_port,
            "--stream",
        ],
        cwd=repo_root,
        stdin=subprocess.DEVNULL,
        stdout=log,
        stderr=subprocess.STDOUT,
        start_new_session=True,
        close_fds=True,
    )
print(process.pid)
PY
  )"

  printf '%s\n' "$pid" >"$pid_file"
  printf '%s\n' "$tilt_port" >"$port_file"
  printf '%s\n' "$repo_root" >"$worktree_file"
  printf '%s\n' "$current_branch" >"$branch_file"
fi

wait_for_tilt_http "$pid"

tilt wait --for=condition=Ready "uiresource/(Tiltfile)" --timeout=60s --port "$tilt_port"
tilt wait --for=condition=Ready "uiresource/controller" --timeout=5m --port "$tilt_port"

echo "YACD dev stack is ready."
echo "Tilt UI: http://${tilt_host}:${tilt_port}/"
echo "Tilt logs: ${log_file}"
