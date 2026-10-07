#!/usr/bin/env bash
set -euo pipefail

BASE=http://localhost:8080

# Keep psql quiet and away from ~/.psqlrc, which is not compatible with
# non-interactive invocation on this machine.
PSQL="psql -X -q -A -t"

# --- log in and return the refresh token (and optionally the access token)
login_refresh() {
  local jar=$1 email=$2 password=$3
  rm -f "$jar"

  curl -s -c "$jar" -b "$jar" "$BASE/web/login" > /dev/null
  local csrf
  csrf=$(awk '/csrf_token/ {print $7}' "$jar")
  [[ -n "$csrf" ]] || { echo "no csrf cookie for $email" >&2; exit 1; }

  curl -s -b "$jar" -c "$jar" \
    -X POST "$BASE/web/login" \
    -d "csrf_token=$csrf" \
    -d "email=$email" \
    -d "password=$password" \
    -o /dev/null

  local refresh
  refresh=$(awk '/refresh_token/ {print $7}' "$jar")
  [[ -n "$refresh" ]] || { echo "login failed for $email" >&2; exit 1; }
  echo "$refresh"
}

# --- admin token for the role-change call
login_access() {
  local jar=$1 email=$2 password=$3
  rm -f "$jar"

  curl -s -c "$jar" -b "$jar" "$BASE/web/login" > /dev/null
  local csrf
  csrf=$(awk '/csrf_token/ {print $7}' "$jar")
  curl -s -b "$jar" -c "$jar" \
    -X POST "$BASE/web/login" \
    -d "csrf_token=$csrf" \
    -d "email=$email" \
    -d "password=$password" \
    -o /dev/null
  awk '/auth_token/ {print $7}' "$jar"
}

# ---------- acquire what we need ----------
ADMIN_TOKEN=$(login_access /tmp/alice.txt alice@example.com password123)
TARGET_REFRESH=$(login_refresh /tmp/target.txt nonaadmin@example.com password123)

TARGET_ID=$($PSQL -d MyWebApp -c "SELECT id FROM users WHERE email = 'nonaadmin@example.com';")
echo "target id: $TARGET_ID"

echo "--- sessions before role change ---"
$PSQL -d MyWebApp -c "
  SELECT jti, revoked_at
  FROM sessions
  WHERE user_id = $TARGET_ID
  ORDER BY id DESC
  LIMIT 3;"

# ---------- change the role ----------
NEW_ROLE=$(curl -s -X PUT "$BASE/api/v1/users/$TARGET_ID/role" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"moderator"}' | jq -r .role)
echo "new role: $NEW_ROLE"

echo "--- sessions after role change (all revoked_at should be set) ---"
$PSQL -d MyWebApp -c "
  SELECT jti, revoked_at
  FROM sessions
  WHERE user_id = $TARGET_ID
  ORDER BY id DESC
  LIMIT 3;"

# ---------- the target's old refresh token should now be rejected ----------
echo "--- refresh with the pre-change token (expect 401) ---"
curl -s -o /dev/null -w 'status: %{http_code}\n' \
  -X POST "$BASE/web/refresh" \
  -H "Cookie: refresh_token=$TARGET_REFRESH"