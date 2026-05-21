#!/usr/bin/env bash
set -euo pipefail

context="kind-yacd-dev"
default_tilt_port="10350"

repo_root="$(git rev-parse --show-toplevel)"
git_common_dir="$(git rev-parse --path-format=absolute --git-common-dir)"
state_dir="$(dirname "$git_common_dir")/.run/yacd-dev"

pid_file="$state_dir/tilt.pid"
port_file="$state_dir/tilt.port"
worktree_file="$state_dir/worktree"

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

wait_for_pid_exit() {
  local pid="$1"
  local attempt

  for attempt in {1..15}; do
    if ! kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done

  return 1
}

tilt_pid="$(read_state_file "$pid_file")"
tilt_port="$(read_state_file "$port_file")"
owner_worktree="$(read_state_file "$worktree_file")"

if [ -z "$tilt_port" ]; then
  tilt_port="$default_tilt_port"
fi

if [ -z "$owner_worktree" ] || [ ! -f "$owner_worktree/Tiltfile" ]; then
  owner_worktree="$repo_root"
fi

if is_tilt_pid "$tilt_pid"; then
  echo "Stopping Tilt pid ${tilt_pid}."
  kill -INT "$tilt_pid" 2>/dev/null || true
  if ! wait_for_pid_exit "$tilt_pid"; then
    echo "Tilt pid ${tilt_pid} did not stop after INT; sending TERM."
    kill -TERM "$tilt_pid" 2>/dev/null || true
    if ! wait_for_pid_exit "$tilt_pid"; then
      echo "Tilt pid ${tilt_pid} did not stop after TERM; sending KILL."
      kill -KILL "$tilt_pid" 2>/dev/null || true
      wait_for_pid_exit "$tilt_pid" || true
    fi
  fi
elif [ -n "$tilt_pid" ]; then
  echo "Ignoring stale or non-Tilt pid ${tilt_pid}."
fi

if kubectl config get-contexts "$context" >/dev/null 2>&1; then
  (
    cd "$owner_worktree"
    tilt down --context "$context" --delete-namespaces
  ) || true
fi

ctlptl delete -f dev/ctlptl.yaml --cascade=true --ignore-not-found
rm -rf "$state_dir"

echo "YACD dev stack is down."
