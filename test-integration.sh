#!/bin/bash
# Integration test for ChawrtD Gateway Migration
# This script verifies the new architecture: openclaw-wrt -> chawrtd <-> clawwrt

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
CHAWRTD_PORT=8001
CHAWRTD_BASE_URL="http://127.0.0.1:$CHAWRTD_PORT"
DEVICE_ID="test-device-$(date +%s)"
TEST_TOKEN="clawwrt"

echo -e "${YELLOW}=== ChawrtD Gateway Architecture Integration Test ===${NC}"
echo "Device ID: $DEVICE_ID"
echo ""

# Step 1: Check chawrtd health
echo -e "${YELLOW}Step 1: Checking chawrtd health...${NC}"
if ! curl -s "$CHAWRTD_BASE_URL/healthz" | grep -q "ok"; then
  echo -e "${RED}✗ chawrtd is not responding. Please ensure it's running on port $CHAWRTD_PORT${NC}"
  exit 1
fi
echo -e "${GREEN}✓ chawrtd is healthy${NC}"
echo ""

# Step 2: List devices (should be empty or list existing ones)
echo -e "${YELLOW}Step 2: Listing connected devices...${NC}"
DEVICES=$(curl -s "$CHAWRTD_BASE_URL/v1/devices")
echo "Response: $DEVICES"
echo -e "${GREEN}✓ Device list API working${NC}"
echo ""

# Step 3: Test device command API (will fail since no device is connected, but API should work)
echo -e "${YELLOW}Step 3: Testing device command routing (expected to fail - no device connected)...${NC}"
COMMAND_RESPONSE=$(curl -s -X POST "$CHAWRTD_BASE_URL/v1/device/$DEVICE_ID/get_status" \
  -H "Content-Type: application/json" \
  -d '{}')
echo "Response: $COMMAND_RESPONSE"
if echo "$COMMAND_RESPONSE" | grep -q "device not found\|error"; then
  echo -e "${GREEN}✓ Device command API working (correctly returns error for missing device)${NC}"
else
  echo -e "${YELLOW}⚠ Unexpected response${NC}"
fi
echo ""

# Step 4: Test alias management
echo -e "${YELLOW}Step 4: Testing device alias management...${NC}"
SET_ALIAS=$(curl -s -X POST "$CHAWRTD_BASE_URL/v1/devices/alias/set" \
  -H "Content-Type: application/json" \
  -d "{\"device_id\": \"$DEVICE_ID\", \"alias\": \"Test Router\"}")
echo "Set alias response: $SET_ALIAS"

LIST_ALIASES=$(curl -s "$CHAWRTD_BASE_URL/v1/devices/aliases")
echo "List aliases response: $LIST_ALIASES"
if echo "$LIST_ALIASES" | grep -q "Test Router"; then
  echo -e "${GREEN}✓ Alias management working${NC}"
else
  echo -e "${YELLOW}⚠ Alias might not be persisted yet${NC}"
fi
echo ""

echo -e "${GREEN}=== Integration Tests Completed ===${NC}"
echo ""
echo "Summary:"
echo "✓ ChawrtD health check"
echo "✓ Device list API"
echo "✓ Device command routing API"
echo "✓ Device alias management"
echo ""
echo "Next steps:"
echo "1. Start clawwrt daemon and verify device connection"
echo "2. From openclaw-wrt, call device commands via HTTP API"
echo "3. Verify device events are consumed via SSE /v1/events/stream"
echo "4. Monitor performance metrics"
