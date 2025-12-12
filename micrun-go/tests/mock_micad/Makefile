# Makefile for Enhanced Mock Micad
#
# This builds the mock_micad with RTOS IO simulation support

CC = gcc
CFLAGS = -Wall -Wextra -std=gnu99 -O2 -g
LDFLAGS = -pthread -lrt

TARGET = mock_micad
SRC = mock_micad.c

.PHONY: all clean clean-all gc run test help ctl

all: $(TARGET)

$(TARGET): $(SRC)
	$(CC) $(CFLAGS) -o $@ $< $(LDFLAGS)

clean:
	rm -f $(TARGET)
	rm mock_ctl

clean-all: clean gc ## Clean build artifacts and leftover resources

gc: ## Garbage collection - cleanup leftover resources
	@echo "🧹 Cleaning up leftover mock_micad resources..."
	@echo "Cleaning up client sockets..."
	@find /tmp/mica -name "*.socket" -type s -delete 2>/dev/null || true
	@echo "Cleaning up PTY symlinks..."
	@find /tmp/mica -name "ttyRPMSG_*" -type l -delete 2>/dev/null || true
	@find /dev -name "ttyRPMSG_*" -type l -delete 2>/dev/null || true
	@echo "Cleaning up main socket..."
	@rm -f /tmp/mica/mica-create.socket
	@echo "Cleaning up socket directory..."
	@rmdir /tmp/mica 2>/dev/null || true
	@echo "✅ Garbage collection completed"

test: $(TARGET)
	@echo "Testing mock_micad..."
	@echo ""
	@echo "NOTE: Make sure Python is installed with socket support."
	@echo ""
	@./$(TARGET) &
	@sleep 2
	@echo ""
	@echo "WARN: Manual testing required - Python test script needs socket module."
	@echo "Use: python3 test_mock.py"
	@echo ""
	@echo "Quick manual test:"
	@echo "  echo 'status' | socat - UNIX-CONNECT:/tmp/mica/mica-create.socket"
	@echo ""
	@sleep 2
	@pkill -f ./$(TARGET) || true
	@echo "Test completed"

run: $(TARGET)
	@echo "Running mock_micad..."
	./$(TARGET)

ctl:
	@echo "Mock micad control helper"
	@echo "  go run ./mock_ctl.go --id <container-id> status"
	@echo "  go run ./mock_ctl.go --id <container-id> start"
	@echo "  go run ./mock_ctl.go --id <container-id> tail"
	@echo "Flags:"
	@echo "  --socket-dir=/tmp/mica        Control socket directory"
	@echo "  --create-socket=/tmp/mica/mica-create.socket  Fallback to mica-create"
	@echo "  --create                       Force use of mica-create socket"

help:
	@echo "Mock Micad Build System"
	@echo ""
	@echo "Targets:"
	@echo "  all    - Build mock_micad"  
	@echo "  run    - Run mock_micad"
	@echo "  ctl    - Show helper usage for mock_ctl.go"
	@echo "  clean  - Remove build artifacts"
	@echo "  clean-all - Clean build artifacts and leftover resources"
	@echo "  gc     - Garbage collection - cleanup leftover resources"
	@echo "  test   - Test mock_micad execution"
	@echo "  help   - Show this help message"
	@echo ""
	@echo "Usage:"
	@echo "  make           # Build mock_micad"
	@echo "  make run       # Run mock_micad"
	@echo "  make test      # Test execution"
	@echo "  make clean-all # Clean build artifacts and leftover resources"
	@echo "  make gc        # Cleanup leftover resources (sockets, PTYs)"
	@echo "  ./mock_micad   # Run with response mode"
	@echo "  ./mock_micad -q # Run in quiet mode (no responses)" 
