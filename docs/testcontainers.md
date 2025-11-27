# Testcontainers

HyperFleet Sentinel uses [testcontainers-go](https://github.com/testcontainers/testcontainers-go/) for integration tests to spin up ephemeral containers for real message broker testing.

## Integration Testing Strategy

The project uses a hybrid approach:

- **Unit tests** (`internal/*/`): Use mocks for fast, isolated testing
- **Integration tests** (`test/integration/`):
  - Mock-based tests for logic verification (fast)
  - Testcontainer-based tests for end-to-end validation (slower, more realistic)

## Running Integration Tests

### With Docker
```bash
make test-integration
```

### With Podman

Testcontainers officially supports Docker only, and some compatibility issues may occur with Podman.

#### Common Error

```
Failed to start RabbitMQ testcontainer: create container: container create: Error response from daemon: container create: unable to find network with name or ID bridge: network not found: creating reaper failed
```

This happens because testcontainers spins up an additional [testcontainers/ryuk](https://github.com/testcontainers/moby-ryuk) container that manages the lifecycle of test containers and performs cleanup.

#### Solution 1: Disable Ryuk (Recommended)

Set the environment variable:

```bash
TESTCONTAINERS_RYUK_DISABLED=true make test-integration
```

Or create `~/.testcontainers.properties`:

```properties
ryuk.disabled=true
```

**Note**: Disabling ryuk means containers won't be automatically cleaned up if tests fail. You may need to manually remove orphaned containers:

```bash
podman ps -a | grep rabbitmq | awk '{print $1}' | xargs podman rm -f
```

#### Solution 2: Enable Ryuk with Elevated Permissions

Ryuk needs root permissions in podman to manage other containers. See [this issue](https://github.com/testcontainers/testcontainers-go/issues/2781#issuecomment-2619626043) for details.

```bash
# Verify socket path inside podman machine
podman machine ssh
ls -al /var/run/podman/podman.sock
exit

# On the host machine (requires sudo)
sudo mkdir -p /var/run/podman
sudo ln -s /Users/YOUR_USER/.local/share/containers/podman/machine/podman.sock /var/run/podman/podman.sock

export DOCKER_HOST="unix:///var/run/podman/podman.sock"
export TESTCONTAINERS_RYUK_CONTAINER_PRIVILEGED=true

# If it still fails, give permissions to the socket
sudo chmod a+xrw /var/run/podman
sudo chmod a+xrw /var/run/podman/podman.sock
```

**Warning**: This solution grants elevated permissions and should only be used in development environments.

## Supported Brokers

The testcontainers infrastructure currently supports:

- **RabbitMQ** ([testcontainers-go/modules/rabbitmq](https://golang.testcontainers.org/modules/rabbitmq/))
- *Future*: Google Pub/Sub via emulator ([testcontainers-go/modules/gcloud](https://golang.testcontainers.org/modules/gcloud/))
- *Future*: AWS SQS via LocalStack ([testcontainers-go/modules/localstack](https://golang.testcontainers.org/modules/localstack/))

## Test Structure

### MockPublisher Tests (Fast)

Used for verifying business logic:

```go
func TestIntegration_EndToEnd(t *testing.T) {
    mockPublisher := &MockPublisher{}
    // ... test logic validation
    // Verify events in mockPublisher.publishedEvents
}
```

### Testcontainer Tests (Realistic)

Used for end-to-end validation with real brokers:

```go
func TestIntegration_EndToEnd_RealBroker(t *testing.T) {
    rabbitMQ, err := NewRabbitMQTestContainer(ctx)
    defer rabbitMQ.Close(ctx)
    // ... test with real broker
}
```

## CI/CD Considerations

In CI environments:

1. Ensure Docker/Podman is available
2. Set `TESTCONTAINERS_RYUK_DISABLED=true` for Podman-based CI
3. Consider running testcontainer tests only on PR merge (they're slower)
4. Use `make test` for fast unit tests in PR validation

## Troubleshooting

### Container Port Conflicts

If you see port conflict errors:

```bash
# Find and stop conflicting containers
podman ps | grep rabbitmq
podman stop <container-id>
```

### Slow Tests

Testcontainer tests are slower than mocks (5-10 seconds for container startup). This is expected and worth the trade-off for real integration testing.

### Container Not Stopping

If containers don't stop after tests:

```bash
# List all containers
podman ps -a

# Remove orphaned test containers
podman rm -f $(podman ps -a -q --filter "ancestor=rabbitmq:3.13-management-alpine")
```

## References

- [Testcontainers for Go Documentation](https://golang.testcontainers.org/)
- [RabbitMQ Module](https://golang.testcontainers.org/modules/rabbitmq/)
- [rh-trex Testcontainers Documentation](https://github.com/openshift-online/rh-trex/blob/main/docs/testcontainers.md)
