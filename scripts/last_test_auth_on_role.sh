# Admin token first
rm -f /tmp/admin.txt
curl -s -c /tmp/admin.txt -b /tmp/admin.txt http://localhost:8080/web/login > /dev/null
CSRF=$(awk '/csrf_token/ {print $7}' /tmp/admin.txt)
curl -s -b /tmp/admin.txt -c /tmp/admin.txt \
  -X POST http://localhost:8080/web/login \
  -d "csrf_token=$CSRF" \
  -d "email=alice@example.com" \
  -d "password=password123" \
  -o /dev/null
ADMIN_TOKEN=$(awk '/auth_token/ {print $7}' /tmp/admin.txt)
echo "admin token length: ${#ADMIN_TOKEN}"

# Non-admin token. Assumes the nonaadmin account exists from a prior
# registration. If it doesn't, register it first:
#   rm -f /tmp/nonadmin.txt
#   curl -s -c /tmp/nonadmin.txt -b /tmp/nonadmin.txt http://localhost:8080/web/register > /dev/null
#   CSRF=$(awk '/csrf_token/ {print $7}' /tmp/nonadmin.txt)
#   curl -s -b /tmp/nonadmin.txt -c /tmp/nonadmin.txt \
#     -X POST http://localhost:8080/web/register \
#     -d "csrf_token=$CSRF" \
#     -d "username=nonaadmin" \
#     -d "email=nonaadmin@example.com" \
#     -d "password=password123" \
#     -d "full_name=Non Admin" \
#     -o /dev/null
USER_TOKEN=$(awk '/auth_token/ {print $7}' /tmp/nonadmin.txt)
echo "user  token length: ${#USER_TOKEN}"

if [[ -z "$USER_TOKEN" ]]; then
  echo "USER_TOKEN is empty; run the registration flow first" >&2
  exit 1
fi

# Now the six non-admin checks
echo "--- non-admin attempts (all should be 403) ---"

printf 'POST   /api/v1/users           %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"escalate@example.com","password":"password123","username":"escalate","full_name":"E","role":"admin"}')"

printf 'PUT    /api/v1/users/1/role    %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  -X PUT http://localhost:8080/api/v1/users/1/role \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"role":"admin"}')"

printf 'GET    /api/v1/users           %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer $USER_TOKEN")"

printf 'GET    /api/v1/users/1         %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  http://localhost:8080/api/v1/users/1 \
  -H "Authorization: Bearer $USER_TOKEN")"

printf 'PUT    /api/v1/users/1         %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  -X PUT http://localhost:8080/api/v1/users/1 \
  -H "Authorization: Bearer $USER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"evil@example.com","username":"alice","full_name":"Hacked"}')"

printf 'DELETE /api/v1/users/1         %s\n' "$(curl -s -o /dev/null -w '%{http_code}' \
  -X DELETE http://localhost:8080/api/v1/users/1 \
  -H "Authorization: Bearer $USER_TOKEN")"

#  Expected output of
#  ./last_test.sh
#
#  admin token length: 251
#  user  token length: 260
#  --- non-admin attempts (all should be 403) ---
#  POST   /api/v1/users           403
#  PUT    /api/v1/users/1/role    403
#  GET    /api/v1/users           403
#  GET    /api/v1/users/1         403
#  PUT    /api/v1/users/1         403
#  DELETE /api/v1/users/1         403
