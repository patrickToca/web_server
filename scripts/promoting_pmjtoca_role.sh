# Get alice's admin token
rm -f /tmp/alice.txt
curl -s -c /tmp/alice.txt -b /tmp/alice.txt http://localhost:8080/web/login > /dev/null
CSRF=$(awk '/csrf_token/ {print $7}' /tmp/alice.txt)
curl -s -b /tmp/alice.txt -c /tmp/alice.txt \
  -X POST http://localhost:8080/web/login \
  -d "csrf_token=$CSRF" \
  -d "email=alice@example.com" \
  -d "password=password123" \
  -o /dev/null
ALICE=$(awk '/auth_token/ {print $7}' /tmp/alice.txt)

# Find pmjtoca's id
PMID=$(psql -d MyWebApp -t -c "SELECT id FROM users WHERE email = 'pmjtoca@gmail.com';" | tr -d ' ')
echo "pmjtoca id: $PMID"

# Promote
curl -s -X PUT "http://localhost:8080/api/v1/users/$PMID/role" \
  -H "Authorization: Bearer $ALICE" \
  -H "Content-Type: application/json" \
  -d '{"role":"admin"}' | jq .