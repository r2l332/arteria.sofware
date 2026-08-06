.PHONY: build run test clean all-docker app-on-host agent-on-host stop logs

# Build the VM app binary for the host
build:
	cd vm-app && go build -o vm-app .

# Run the VM app natively (for app-on-host mode)
run: build
	cd vm-app && SEND_ADDR=$${SEND_ADDR:-localhost:12575} \
		RECV_ADDR=$${RECV_ADDR:-:2576} \
		WEB_PORT=$${WEB_PORT:-8090} \
		./vm-app run

# Run E2E tests natively
test: build
	cd vm-app && SEND_ADDR=$${SEND_ADDR:-localhost:12575} \
		RECV_ADDR=$${RECV_ADDR:-:2576} \
		TEST_COUNT=$${TEST_COUNT:-5} \
		CONNECT_TIMEOUT_S=$${CONNECT_TIMEOUT_S:-15} \
		./vm-app test

# Send a single message (quick connectivity check)
send-one: build
	cd vm-app && SEND_ADDR=$${SEND_ADDR:-localhost:12575} ./vm-app send-one

# --- Docker modes ---

all-docker:
	./run-demo.sh all-docker

app-on-host:
	./run-demo.sh app-on-host

agent-on-host:
	./run-demo.sh agent-on-host

stop:
	./run-demo.sh stop

logs:
	./run-demo.sh logs

test-docker:
	./run-demo.sh test-docker

clean:
	rm -f vm-app/vm-app
	./run-demo.sh stop 2>/dev/null || true
