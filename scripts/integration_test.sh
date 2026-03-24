#!/bin/bash
set -e

PASS=0
FAIL=0
TOTAL=0

check() {
  TOTAL=$((TOTAL+1))
  local name="$1"
  local expected="$2"
  local actual="$3"
  if echo "$actual" | grep -q "$expected"; then
    PASS=$((PASS+1))
    echo "  PASS [$TOTAL] $name"
  else
    FAIL=$((FAIL+1))
    echo "  FAIL [$TOTAL] $name"
    echo "    expected: $expected"
    echo "    got: $(echo "$actual" | head -c 200)"
  fi
}

echo "================================================"
echo "  INTEGRATION TEST — Phase 1 Core Messaging"
echo "================================================"

echo ""
echo "--- AUTH ---"

R=$(curl -s -X POST http://localhost:8081/auth/bootstrap -H "Content-Type: application/json" -d '{"email":"admin@orbit.test","password":"securepass123","display_name":"Admin"}')
check "T1 Bootstrap admin" '"role":"admin"' "$R"

R=$(curl -s -X POST http://localhost:8081/auth/bootstrap -H "Content-Type: application/json" -d '{"email":"x@x.com","password":"12345678","display_name":"X"}')
check "T2 Bootstrap dup -> 403" '"status":403' "$R"

R=$(curl -s -X POST http://localhost:8081/auth/bootstrap -H "Content-Type: application/json" -d '{"email":"bad","password":"12345678","display_name":"X"}')
check "T3 Bootstrap bad email -> 400" '"status":400' "$R"

LOGIN=$(curl -s -X POST http://localhost:8081/auth/login -H "Content-Type: application/json" -d '{"email":"admin@orbit.test","password":"securepass123"}')
TOKEN=$(echo "$LOGIN" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
ADMIN_ID=$(echo "$LOGIN" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
check "T4 Login admin" '"access_token"' "$LOGIN"

R=$(curl -s -X POST http://localhost:8081/auth/login -H "Content-Type: application/json" -d '{"email":"admin@orbit.test","password":"wrong"}')
check "T5 Login wrong pass -> 401" '"status":401' "$R"

R=$(curl -s -X POST http://localhost:8081/auth/login -H "Content-Type: application/json" -d '{"email":"nobody@x.com","password":"12345678"}')
check "T6 Login nonexistent -> 401" '"status":401' "$R"

R=$(curl -s http://localhost:8081/auth/me -H "Authorization: Bearer $TOKEN")
check "T7 GET /auth/me" '"email":"admin@orbit.test"' "$R"

R=$(curl -s http://localhost:8081/auth/me)
check "T8 GET /auth/me no token -> 401" '"status":401' "$R"

R=$(curl -s http://localhost:8081/auth/me -H "Authorization: Bearer badtoken")
check "T9 GET /auth/me bad token -> 401" '"status":401' "$R"

R=$(curl -s http://localhost:8081/auth/sessions -H "Authorization: Bearer $TOKEN")
check "T10 GET /auth/sessions" '"sessions"' "$R"

INVITE_R=$(curl -s -X POST http://localhost:8081/auth/invites -H "Content-Type: application/json" -H "Authorization: Bearer $TOKEN" -d '{"role":"member","max_uses":5}')
INVITE_CODE=$(echo "$INVITE_R" | grep -o '"code":"[^"]*"' | cut -d'"' -f4)
check "T11 Create invite" '"code"' "$INVITE_R"

R=$(curl -s -X POST http://localhost:8081/auth/invite/validate -H "Content-Type: application/json" -d "{\"code\":\"$INVITE_CODE\"}")
check "T12 Validate invite" '"valid":true' "$R"

R=$(curl -s -X POST http://localhost:8081/auth/invite/validate -H "Content-Type: application/json" -d '{"code":"nonexistent"}')
check "T13 Validate bad invite -> 404" '"status":404' "$R"

R=$(curl -s http://localhost:8081/auth/invites -H "Authorization: Bearer $TOKEN")
check "T14 List invites" '"invites"' "$R"

REG=$(curl -s -X POST http://localhost:8081/auth/register -H "Content-Type: application/json" -d "{\"invite_code\":\"$INVITE_CODE\",\"email\":\"user1@orbit.test\",\"password\":\"userpass123\",\"display_name\":\"User One\"}")
U1_ID=$(echo "$REG" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
check "T15 Register user1" '"email":"user1@orbit.test"' "$REG"

R=$(curl -s -X POST http://localhost:8081/auth/register -H "Content-Type: application/json" -d "{\"invite_code\":\"$INVITE_CODE\",\"email\":\"user1@orbit.test\",\"password\":\"userpass123\",\"display_name\":\"Dup\"}")
check "T16 Register dup email -> 409" '"status":409' "$R"

U1_LOGIN=$(curl -s -X POST http://localhost:8081/auth/login -H "Content-Type: application/json" -d '{"email":"user1@orbit.test","password":"userpass123"}')
U1_TOKEN=$(echo "$U1_LOGIN" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
check "T17 Login user1" '"access_token"' "$U1_LOGIN"

R=$(curl -s -X POST http://localhost:8081/auth/reset-admin -H "Content-Type: application/json" -d '{"reset_key":"wrong","email":"admin@orbit.test","new_password":"newpassword1"}')
check "T18 Reset admin wrong key -> 403" '"status":403' "$R"

R=$(curl -s -X POST http://localhost:8081/auth/invites -H "Content-Type: application/json" -H "Authorization: Bearer $U1_TOKEN" -d '{"role":"member"}')
check "T19 Non-admin invite -> 403" '"status":403' "$R"

echo ""
echo "--- MESSAGING ---"

DM=$(curl -s -X POST http://localhost:8082/chats/direct -H "Content-Type: application/json" -H "X-User-ID: $ADMIN_ID" -d "{\"user_id\":\"$U1_ID\"}")
CHAT_ID=$(echo "$DM" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
check "T20 Create DM" '"type":"direct"' "$DM"

DM2=$(curl -s -X POST http://localhost:8082/chats/direct -H "Content-Type: application/json" -H "X-User-ID: $ADMIN_ID" -d "{\"user_id\":\"$U1_ID\"}")
CHAT_ID2=$(echo "$DM2" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
TOTAL=$((TOTAL+1))
if [ "$CHAT_ID" = "$CHAT_ID2" ]; then PASS=$((PASS+1)); echo "  PASS [$TOTAL] T21 DM dedup"; else FAIL=$((FAIL+1)); echo "  FAIL [$TOTAL] T21 DM dedup"; fi

R=$(curl -s -X POST http://localhost:8082/chats/direct -H "Content-Type: application/json" -H "X-User-ID: $ADMIN_ID" -d "{\"user_id\":\"$ADMIN_ID\"}")
check "T22 Self-DM -> 400" '"status":400' "$R"

MSG=$(curl -s -X POST "http://localhost:8082/chats/$CHAT_ID/messages" -H "Content-Type: application/json" -H "X-User-ID: $ADMIN_ID" -d '{"content":"Hello World!"}')
MSG_ID=$(echo "$MSG" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
check "T23 Send message" '"content":"Hello World!"' "$MSG"

REPLY=$(curl -s -X POST "http://localhost:8082/chats/$CHAT_ID/messages" -H "Content-Type: application/json" -H "X-User-ID: $U1_ID" -d "{\"content\":\"Reply!\",\"reply_to_id\":\"$MSG_ID\"}")
REPLY_ID=$(echo "$REPLY" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
check "T24 Send reply" '"reply_to_id"' "$REPLY"

R=$(curl -s -X POST "http://localhost:8082/chats/$CHAT_ID/messages" -H "Content-Type: application/json" -H "X-User-ID: $ADMIN_ID" -d '{"content":""}')
check "T25 Send empty -> 400" '"status":400' "$R"

R=$(curl -s -X POST "http://localhost:8082/chats/$CHAT_ID/messages" -H "Content-Type: application/json" -d '{"content":"test"}')
check "T26 Send no auth -> 401" '"status":401' "$R"

R=$(curl -s "http://localhost:8082/chats/$CHAT_ID/messages" -H "X-User-ID: $ADMIN_ID")
check "T27 List messages" '"data"' "$R"

R=$(curl -s -X PATCH "http://localhost:8082/messages/$MSG_ID" -H "Content-Type: application/json" -H "X-User-ID: $ADMIN_ID" -d '{"content":"Edited!"}')
check "T28 Edit message" '"is_edited":true' "$R"

R=$(curl -s -X PATCH "http://localhost:8082/messages/$MSG_ID" -H "Content-Type: application/json" -H "X-User-ID: $U1_ID" -d '{"content":"Hacked!"}')
check "T29 Edit not author -> 403" '"status":403' "$R"

R=$(curl -s -X POST "http://localhost:8082/chats/$CHAT_ID/pin/$MSG_ID" -H "X-User-ID: $ADMIN_ID")
check "T30 Pin message" '"Message pinned"' "$R"

R=$(curl -s "http://localhost:8082/chats/$CHAT_ID/pinned" -H "X-User-ID: $ADMIN_ID")
check "T31 List pinned" '"is_pinned":true' "$R"

R=$(curl -s -X DELETE "http://localhost:8082/chats/$CHAT_ID/pin/$MSG_ID" -H "X-User-ID: $ADMIN_ID")
check "T32 Unpin message" '"Message unpinned"' "$R"

curl -s -X POST "http://localhost:8082/chats/$CHAT_ID/pin/$MSG_ID" -H "X-User-ID: $ADMIN_ID" > /dev/null
R=$(curl -s -X DELETE "http://localhost:8082/chats/$CHAT_ID/pin" -H "X-User-ID: $ADMIN_ID")
check "T33 Unpin all DM" '"All messages unpinned"' "$R"

R=$(curl -s -X PATCH "http://localhost:8082/chats/$CHAT_ID/read" -H "Content-Type: application/json" -H "X-User-ID: $U1_ID" -d "{\"last_read_message_id\":\"$REPLY_ID\"}")
check "T34 Mark read" '"Read pointer updated"' "$R"

GROUP=$(curl -s -X POST http://localhost:8082/chats -H "Content-Type: application/json" -H "X-User-ID: $ADMIN_ID" -d '{"name":"Test Group","description":"test"}')
GROUP_ID=$(echo "$GROUP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
check "T35 Create group" '"type":"group"' "$GROUP"

R=$(curl -s -X POST "http://localhost:8082/messages/forward" -H "Content-Type: application/json" -H "X-User-ID: $ADMIN_ID" -d "{\"message_ids\":[\"$MSG_ID\"],\"to_chat_id\":\"$GROUP_ID\"}")
check "T36 Forward messages" '"is_forwarded":true' "$R"

R=$(curl -s -X POST "http://localhost:8082/messages/forward" -H "Content-Type: application/json" -H "X-User-ID: $U1_ID" -d "{\"message_ids\":[\"$MSG_ID\"],\"to_chat_id\":\"$GROUP_ID\"}")
check "T37 Forward non-member -> 403" '"status":403' "$R"

R=$(curl -s -X DELETE "http://localhost:8082/messages/$REPLY_ID" -H "X-User-ID: $U1_ID")
check "T38 Delete message" '"Message deleted"' "$R"

R=$(curl -s "http://localhost:8082/chats/$CHAT_ID" -H "X-User-ID: $ADMIN_ID")
check "T39 Get chat" '"type":"direct"' "$R"

R=$(curl -s "http://localhost:8082/chats/$GROUP_ID" -H "X-User-ID: $U1_ID")
check "T40 Get chat non-member -> 403" '"status":403' "$R"

R=$(curl -s "http://localhost:8082/chats/$CHAT_ID/members" -H "X-User-ID: $ADMIN_ID")
check "T41 Get members" '"data"' "$R"

R=$(curl -s "http://localhost:8082/chats" -H "X-User-ID: $ADMIN_ID")
check "T42 List chats" '"data"' "$R"

echo ""
echo "--- USERS ---"

R=$(curl -s "http://localhost:8082/users/me" -H "X-User-ID: $ADMIN_ID")
check "T43 GET /users/me" '"email":"admin@orbit.test"' "$R"

R=$(curl -s "http://localhost:8082/users/$U1_ID" -H "X-User-ID: $ADMIN_ID")
check "T44 GET /users/:id" '"email":"user1@orbit.test"' "$R"

R=$(curl -s "http://localhost:8082/users?q=admin" -H "X-User-ID: $ADMIN_ID")
check "T45 Search users" '"users"' "$R"

R=$(curl -s "http://localhost:8082/users?q=" -H "X-User-ID: $ADMIN_ID")
check "T46 Search empty -> 400" '"status":400' "$R"

R=$(curl -s -X PUT "http://localhost:8082/users/me" -H "Content-Type: application/json" -H "X-User-ID: $U1_ID" -d '{"display_name":"Updated Name"}')
check "T47 Update profile" '"display_name":"Updated Name"' "$R"

echo ""
echo "--- LINK PREVIEW ---"

R=$(curl -s "http://localhost:8082/messages/link-preview?url=https://example.com" -H "X-User-ID: $ADMIN_ID")
check "T51 Link preview" '"preview"' "$R"

R=$(curl -s "http://localhost:8082/messages/link-preview" -H "X-User-ID: $ADMIN_ID")
check "T52 Link preview no URL -> 400" '"status":400' "$R"

echo ""
echo "--- GATEWAY PROXY ---"

R=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" -d '{"email":"admin@orbit.test","password":"securepass123"}')
GW_TOKEN=$(echo "$R" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
check "T48 Gateway -> auth" '"access_token"' "$R"

R=$(curl -s "http://localhost:8080/api/v1/chats" -H "Authorization: Bearer $GW_TOKEN")
check "T49 Gateway -> messaging" '"data"' "$R"

R=$(curl -s "http://localhost:8080/api/v1/chats")
check "T50 Gateway no auth -> 401" '"status":401' "$R"

echo ""
echo "================================================"
echo "  RESULTS: $PASS passed, $FAIL failed, $TOTAL total"
echo "================================================"
if [ $FAIL -gt 0 ]; then exit 1; fi
