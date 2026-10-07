#!/bin/bash

BASE_URL="http://localhost:8080"
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Generate unique identifiers
TIMESTAMP=$(date +%s)
RANDOM_SUFFIX=$((RANDOM % 1000))

echo -e "${YELLOW}=== Testing MyWebApp API ===${NC}\n"

# Check if server is running
echo -e "${BLUE}Checking if server is running...${NC}"
if ! curl -s --connect-timeout 2 $BASE_URL/health > /dev/null; then
    echo -e "${RED}ERROR: Server is not running on $BASE_URL${NC}"
    echo -e "${YELLOW}Please start the server first: make run${NC}"
    exit 1
fi
echo -e "${GREEN}Server is running!${NC}\n"

# 1. Health Check
echo -e "${GREEN}1. Testing Health Check...${NC}"
curl -s $BASE_URL/health | jq '.'
echo -e "\n"

# 2. Create Users with unique usernames
echo -e "${GREEN}2. Creating Users with unique usernames...${NC}"

# Create user 1 with unique username
USERNAME1="alice_${TIMESTAMP}"
echo -e "${BLUE}Creating User 1 with username: $USERNAME1${NC}"
USER1=$(curl -s -X POST $BASE_URL/api/v1/users \
  -H "Content-Type: application/json" \
  -d "{
    \"email\": \"alice_${TIMESTAMP}@example.com\",
    \"password\": \"AlicePass123\",
    \"username\": \"$USERNAME1\",
    \"full_name\": \"Alice Johnson ${TIMESTAMP}\",
    \"role\": \"user\"
  }")

if [ -z "$USER1" ]; then
    echo -e "${RED}No response from server for User 1${NC}"
else
    echo $USER1 | jq '.'
    USER1_ID=$(echo $USER1 | jq -r '.id // empty')
    if [ -z "$USER1_ID" ] || [ "$USER1_ID" = "null" ]; then
        echo -e "${RED}Failed to create User 1. Error: $(echo $USER1 | jq -r '.error // "unknown error"')${NC}"
        USER1_ID=""
    else
        echo -e "${GREEN}User 1 created with ID: $USER1_ID${NC}"
    fi
fi
echo -e "\n"

# Create user 2 with unique username
USERNAME2="bob_${TIMESTAMP}"
echo -e "${BLUE}Creating User 2 with username: $USERNAME2${NC}"
USER2=$(curl -s -X POST $BASE_URL/api/v1/users \
  -H "Content-Type: application/json" \
  -d "{
    \"email\": \"bob_${TIMESTAMP}@example.com\",
    \"password\": \"BobPass123\",
    \"username\": \"$USERNAME2\",
    \"full_name\": \"Bob Smith ${TIMESTAMP}\",
    \"role\": \"user\"
  }")

if [ -z "$USER2" ]; then
    echo -e "${RED}No response from server for User 2${NC}"
else
    echo $USER2 | jq '.'
    USER2_ID=$(echo $USER2 | jq -r '.id // empty')
    if [ -z "$USER2_ID" ] || [ "$USER2_ID" = "null" ]; then
        echo -e "${RED}Failed to create User 2. Error: $(echo $USER2 | jq -r '.error // "unknown error"')${NC}"
        USER2_ID=""
    else
        echo -e "${GREEN}User 2 created with ID: $USER2_ID${NC}"
    fi
fi
echo -e "\n"

# Create admin user with unique username
ADMIN_USERNAME="admin_${TIMESTAMP}"
echo -e "${BLUE}Creating Admin User with username: $ADMIN_USERNAME${NC}"
ADMIN=$(curl -s -X POST $BASE_URL/api/v1/users \
  -H "Content-Type: application/json" \
  -d "{
    \"email\": \"admin_${TIMESTAMP}@example.com\",
    \"password\": \"AdminPass123\",
    \"username\": \"$ADMIN_USERNAME\",
    \"full_name\": \"Admin User ${TIMESTAMP}\",
    \"role\": \"admin\"
  }")

if [ -z "$ADMIN" ]; then
    echo -e "${RED}No response from server for Admin User${NC}"
else
    echo $ADMIN | jq '.'
    ADMIN_ID=$(echo $ADMIN | jq -r '.id // empty')
    if [ -z "$ADMIN_ID" ] || [ "$ADMIN_ID" = "null" ]; then
        echo -e "${RED}Failed to create Admin. Error: $(echo $ADMIN | jq -r '.error // "unknown error"')${NC}"
        ADMIN_ID=""
    else
        echo -e "${GREEN}Admin created with ID: $ADMIN_ID${NC}"
    fi
fi
echo -e "\n"

# Check if we have any IDs to continue
if [ -z "$USER1_ID" ]; then
    echo -e "${RED}Cannot continue tests - no users were created successfully${NC}"
    echo -e "${YELLOW}Check if the server is running and the database is properly set up${NC}"
    echo -e "${YELLOW}Try running: curl -X POST $BASE_URL/api/v1/users -H 'Content-Type: application/json' -d '{\"email\":\"test@test.com\",\"password\":\"password123\",\"username\":\"testuser\",\"full_name\":\"Test User\"}'${NC}"
    exit 1
fi

# 3. List All Users
echo -e "${GREEN}3. Listing All Users...${NC}"
LIST=$(curl -s "$BASE_URL/api/v1/users?page=1&limit=20")
if [ -z "$LIST" ]; then
    echo -e "${RED}No response from list users${NC}"
else
    echo $LIST | jq '.'
    COUNT=$(echo $LIST | jq -r '.data | length // 0')
    echo -e "${GREEN}Total users: $COUNT${NC}"
fi
echo -e "\n"

# 4. Get Specific User
if [ -n "$USER1_ID" ]; then
    echo -e "${GREEN}4. Getting User by ID ($USER1_ID)...${NC}"
    GET_USER=$(curl -s $BASE_URL/api/v1/users/$USER1_ID)
    if [ -z "$GET_USER" ]; then
        echo -e "${RED}No response from get user${NC}"
    else
        echo $GET_USER | jq '.'
    fi
    echo -e "\n"
fi

# 5. Update User
if [ -n "$USER1_ID" ]; then
    echo -e "${GREEN}5. Updating User ($USER1_ID)...${NC}"
    UPDATE=$(curl -s -X PUT $BASE_URL/api/v1/users/$USER1_ID \
      -H "Content-Type: application/json" \
      -d "{
        \"email\": \"alice_updated_${TIMESTAMP}@example.com\",
        \"username\": \"alice_updated_${TIMESTAMP}\",
        \"full_name\": \"Alice Johnson Updated ${TIMESTAMP}\",
        \"role\": \"admin\",
        \"is_active\": true
      }")
    if [ -z "$UPDATE" ]; then
        echo -e "${RED}No response from update user${NC}"
    else
        echo $UPDATE | jq '.'
    fi
    echo -e "\n"
fi

# 6. Deactivate User
if [ -n "$USER2_ID" ]; then
    echo -e "${GREEN}6. Deactivating User ($USER2_ID)...${NC}"
    DEACTIVATE=$(curl -s -X PUT $BASE_URL/api/v1/users/$USER2_ID \
      -H "Content-Type: application/json" \
      -d "{
        \"email\": \"bob_${TIMESTAMP}@example.com\",
        \"username\": \"bob_${TIMESTAMP}\",
        \"full_name\": \"Bob Smith ${TIMESTAMP}\",
        \"role\": \"user\",
        \"is_active\": false
      }")
    if [ -z "$DEACTIVATE" ]; then
        echo -e "${RED}No response from deactivate user${NC}"
    else
        echo $DEACTIVATE | jq '.'
    fi
    echo -e "\n"
fi

# 7. List Users Again (see changes)
echo -e "${GREEN}7. Listing Users After Updates (first 5 names)...${NC}"
LIST2=$(curl -s "$BASE_URL/api/v1/users?page=1&limit=20")
if [ -z "$LIST2" ]; then
    echo -e "${RED}No response from list users${NC}"
else
    echo $LIST2 | jq -r '.data[0:5][].full_name'
fi
echo -e "\n"

# 8. Delete User
if [ -n "$USER2_ID" ]; then
    echo -e "${GREEN}8. Deleting User ($USER2_ID)...${NC}"
    DELETE=$(curl -s -X DELETE $BASE_URL/api/v1/users/$USER2_ID)
    if [ -z "$DELETE" ]; then
        echo -e "${GREEN}User $USER2_ID deleted successfully (no content response)${NC}"
    else
        echo "Delete response: $DELETE"
    fi
    echo -e "\n"
fi

# 9. Verify Deletion
if [ -n "$USER2_ID" ]; then
    echo -e "${GREEN}9. Verifying User Deletion...${NC}"
    VERIFY=$(curl -s $BASE_URL/api/v1/users/$USER2_ID)
    if [ -z "$VERIFY" ]; then
        echo -e "${RED}No response from verification${NC}"
    else
        echo $VERIFY | jq '.'
    fi
    echo -e "\n"
fi

# 10. Test Error Cases
echo -e "${GREEN}10. Testing Error Cases...${NC}"

echo "Getting non-existent user (should return 404):"
NONEXISTENT=$(curl -s $BASE_URL/api/v1/users/99999)
if [ -z "$NONEXISTENT" ]; then
    echo -e "${RED}No response${NC}"
else
    echo $NONEXISTENT | jq '.'
fi
echo -e "\n"

echo "Creating user with invalid email (should return validation error):"
INVALID=$(curl -s -X POST $BASE_URL/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{
    "email": "invalid-email",
    "password": "password123",
    "username": "testuser",
    "full_name": "Test User"
  }')
if [ -z "$INVALID" ]; then
    echo -e "${RED}No response${NC}"
else
    echo $INVALID | jq '.'
fi
echo -e "\n"

echo "Creating user with short password (should return validation error):"
SHORT_PASS=$(curl -s -X POST $BASE_URL/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "123",
    "username": "testuser2",
    "full_name": "Test User 2"
  }')
if [ -z "$SHORT_PASS" ]; then
    echo -e "${RED}No response${NC}"
else
    echo $SHORT_PASS | jq '.'
fi
echo -e "\n"

echo "Creating user with missing full_name (should return validation error):"
MISSING_FIELD=$(curl -s -X POST $BASE_URL/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "password123",
    "username": "testuser3"
  }')
if [ -z "$MISSING_FIELD" ]; then
    echo -e "${RED}No response${NC}"
else
    echo $MISSING_FIELD | jq '.'
fi
echo -e "\n"

echo -e "${GREEN}=== Test Complete ===${NC}"
echo -e "${YELLOW}Summary:${NC}"
echo -e "  - Created User 1: $([ -n "$USER1_ID" ] && echo "✅ Success (ID: $USER1_ID)" || echo "❌ Failed")"
echo -e "  - Created User 2: $([ -n "$USER2_ID" ] && echo "✅ Success (ID: $USER2_ID)" || echo "❌ Failed")"
echo -e "  - Created Admin: $([ -n "$ADMIN_ID" ] && echo "✅ Success (ID: $ADMIN_ID)" || echo "❌ Failed")"
echo -e "  - Deleted User 2: $([ -n "$USER2_ID" ] && echo "✅ Success" || echo "❌ Failed")"