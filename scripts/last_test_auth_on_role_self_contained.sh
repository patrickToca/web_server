#!/usr/bin/env bash
set -euo pipefail

BASE=http://localhost:8080

# --- helper: log in and echo the access token -----------------------------
login() {
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

  local token
  token=$(awk '/auth_token/ {print $7}' "$jar")
  [[ -n "$token" ]] || { echo "login failed for $email" >&2; exit 1; }
  echo "$token"
}

# --- helper: register a throwaway user (idempotent enough for a dev box) ---
register() {
  local jar=$1 username=$2 email=$3 password=$4
  rm -f "$jar"

  curl -s -c "$jar" -b "$jar" "$BASE/web/register" > /dev/null
  local csrf
  csrf=$(awk '/csrf_token/ {print $7}' "$jar")
  [[ -n "$csrf" ]] || { echo "no csrf cookie for register" >&2; exit 1; }

  curl -s -b "$jar" -c "$jar" \
    -X POST "$BASE/web/register" \
    -d "csrf_token=$csrf" \
    -d "username=$username" \
    -d "email=$email" \
    -d "password=$password" \
    -d "full_name=Throwaway" \
    -o /dev/null
}

# --- acquire both tokens -------------------------------------------------
ADMIN_TOKEN=$(login /tmp/admin.txt alice@example.com password123)

# Register the non-admin, then log in to obtain its token. If the
# account already exists the register POST returns an error page and
# login below fails; in that case the account is already set up, and
# the login succeeds directly.
register /tmp/nonadmin.txt nonaadmin nonaadmin@example.com password123
USER_TOKEN=$(login /tmp/user.txt nonaadmin@example.com password123)

echo "admin token length: ${#ADMIN_TOKEN}"
echo "user  token length: ${#USER_TOKEN}"

# --- sanity: decode the roles from the tokens ----------------------------
decode_role() {
  local tok=$1
  # JWT payload is the second dot-separated segment, base64url.
  # Pad to a multiple of 4 for `base64 -d`.
  local payload
  payload=$(echo "$tok" | cut -d. -f2)
  local pad=$(( (4 - ${#payload} % 4) % 4 ))
  payload="$payload$(printf '=%.0s' $(seq 1 $pad))"
  echo "$payload" | tr '_-' '/+' | base64 -d 2>/dev/null | jq -r .role
}

echo "admin role claim: $(decode_role "$ADMIN_TOKEN")"
echo "user  role claim: $(decode_role "$USER_TOKEN")"

# --- the checks ----------------------------------------------------------
echo
echo "--- non-admin attempts (all should be 403) ---"

printf 'POST   /api/v1/users           %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST "$BASE/api/v1/users" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"escalate@example.com","password":"password123","username":"escalate","full_name":"E","role":"admin"}')"

printf 'PUT    /api/v1/users/1/role    %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  -X PUT "$BASE/api/v1/users/1/role" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"admin"}')"

printf 'GET    /api/v1/users           %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  "$BASE/api/v1/users" \
  -H "Authorization: Bearer $USER_TOKEN")"

printf 'GET    /api/v1/users/1         %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  "$BASE/api/v1/users/1" \
  -H "Authorization: Bearer $USER_TOKEN")"

printf 'PUT    /api/v1/users/1         %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  -X PUT "$BASE/api/v1/users/1" \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"evil@example.com","username":"alice","full_name":"Hacked"}')"

printf 'DELETE /api/v1/users/1         %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  -X DELETE "$BASE/api/v1/users/1" \
  -H "Authorization: Bearer $USER_TOKEN")"

echo
echo "--- admin attempts (both should be 200) ---"

printf 'GET    /api/v1/users           %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  "$BASE/api/v1/users" \
  -H "Authorization: Bearer $ADMIN_TOKEN")"

printf 'GET    /api/v1/users/1         %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  "$BASE/api/v1/users/1" \
  -H "Authorization: Bearer $ADMIN_TOKEN")"