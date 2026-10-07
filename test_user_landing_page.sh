BASE=http://localhost:8080
JAR=/tmp/user.txt

# Get a fresh CSRF cookie and token
rm -f "$JAR"
curl -s -c "$JAR" -b "$JAR" "$BASE/web/login" > /dev/null
CSRF=$(awk '/csrf_token/ {print $7}' "$JAR")

# Test 1: log in as non-admin; the redirect should land on /web/me
echo "--- test 1: non-admin login ---"
curl -sL -c "$JAR" -b "$JAR" \
  -d "csrf_token=$CSRF" \
  -d "email=nonaadmin@example.com" \
  -d "password=password123" \
  -o /dev/null \
  -w 'final URL: %{url_effective}\nstatus:    %{http_code}\n' \
  "$BASE/web/login"

# Test 2: non-admin navigates to /web/users; the redirect should land on /web/me
echo "--- test 2: non-admin hitting /web/users ---"
curl -sL -b "$JAR" -c "$JAR" \
  -o /dev/null \
  -w 'final URL: %{url_effective}\nstatus:    %{http_code}\n' \
  "$BASE/web/users"