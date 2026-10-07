# 1. Fetch the login page, capture the CSRF cookie
curl -s -c /tmp/cookies.txt -b /tmp/cookies.txt \
  http://localhost:8080/web/login > /dev/null

echo "--- CSRF cookie ---"
grep csrf_token /tmp/cookies.txt

# 2. Extract the token from the cookie jar
CSRF=$(awk '/csrf_token/ {print $7}' /tmp/cookies.txt)
echo "--- token: $CSRF ---"

# 3. Submit the login form with both the cookie and the form field
curl -s -c /tmp/cookies.txt -b /tmp/cookies.txt \
  -X POST http://localhost:8080/web/login \
  -d "csrf_token=$CSRF" \
  -d "email=alice@example.com" \
  -d "password=password123" \
  -i | head -20