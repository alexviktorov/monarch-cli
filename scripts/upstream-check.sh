#!/usr/bin/env bash
# Check the UPSTREAM.md watchlist for new commits/issues that smell like
# Monarch API breakage. Requires the gh CLI. Exits non-zero when an
# auth-flagged commit is found, so it composes with automation.
#
# Policy reminder: upstream is read for UNDERSTANDING only. Verify any fix
# against the live API and re-implement it here; never import upstream code.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
state_file="$repo_root/.upstream-state"
since="$(cat "$state_file" 2>/dev/null || echo 2026-08-01T00:00:00Z)"
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

repos=(
  hammem/monarchmoney
  eshaffer321/monarch-go
  robcerda/monarch-mcp-server
  thedavidweng/monarchmoney-cli
  wesm/moneyflow
  modelcontextprotocol/go-sdk
)

flagged=0
for repo in "${repos[@]}"; do
  echo "== $repo (since $since) =="
  commits="$(gh api "repos/$repo/commits?since=$since" \
    --jq '.[] | .sha[0:7] + "  " + (.commit.message | split("\n")[0])' 2>/dev/null || true)"
  if [ -n "$commits" ]; then
    echo "$commits"
    if grep -qiE 'auth|login|header|device|base.?url|graphql|cloudflare|403|404' <<<"$commits"; then
      echo "  ^^ AUTH-RELATED: read the diff, verify against the live API, re-implement here"
      flagged=1
    fi
  else
    echo "  (no new commits)"
  fi
  echo
done

echo "== recent breakage chatter (hammem/monarchmoney issues) =="
gh search issues --repo hammem/monarchmoney --sort created --limit 5 \
  "403 OR 404 OR login OR broken" \
  --json title,url,createdAt \
  --jq '.[] | .createdAt[0:10] + "  " + .title + "  " + .url' || true

echo "$now" >"$state_file"
exit "$flagged"
