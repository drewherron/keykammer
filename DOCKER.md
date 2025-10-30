# Keykammer Docker Guide

This document explains how to run Keykammer using Docker containers.

## Quick Start

### Build the Docker Image

```bash
# Build with default version
docker build -t keykammer .

# Build with custom version
docker build \
  --build-arg VERSION=0.3.0-alpha \
  --build-arg BUILD_TIME="$(date -u '+%Y-%m-%d %H:%M:%S UTC')" \
  --build-arg GIT_COMMIT="$(git rev-parse --short HEAD)" \
  -t keykammer:0.3.0-alpha .
```

### Run Single Container

```bash
# Run discovery server
docker run -p 53952:53952 keykammer -discovery-server-mode

# Run client with keyfile (mount from host)
docker run -it -v /path/to/keyfile.txt:/app/keyfile.txt keykammer \
  /app/keyfile.txt -discovery-server http://host.docker.internal:53952
```

## Production Discovery Server

Running a discovery server will probably be the most common use case for Docker here.

```bash
# Run discovery server in production
docker run -d \
  --name keykammer-discovery \
  --restart unless-stopped \
  -p 53952:53952 \
  keykammer:0.3.0-alpha \
  -discovery-server-mode
```

## Docker Compose

The included `docker-compose.yml` provides a production-ready discovery server:

```bash
# Start discovery server
docker-compose up -d --build

# Check status
docker-compose ps

# View logs
docker-compose logs -f

# Stop server
docker-compose down
```

Clients then connect with: `-discovery-server http://your-server:53952`

## Configuration

### Using Config Files

Mount custom configuration files into the container:

```bash
docker run -it \
  -v /path/to/keykammer.yaml:/app/keykammer.yaml \
  -v /path/to/keyfile.txt:/app/keyfile.txt \
  keykammer
```

### Environment Variables

Pass configuration through command line arguments:

```bash
docker run -it \
  -v /path/to/keyfile.txt:/app/keyfile.txt \
  keykammer \
  /app/keyfile.txt \
  -discovery-server http://discovery.example.com:53952 \
  -port 53952
```

## Example Files

The `examples/` directory contains:

- `example-keyfile.txt` - Sample keyfile for testing
- `alice-config.yaml` - Configuration for Alice client
- `bob-config.yaml` - Configuration for Bob client

## Networking

### Port Mapping

- **53952**: Default Keykammer port (map with `-p 53952:53952`)
- Clients need to reach the discovery server
- P2P connections happen directly between clients

### Discovery Server

The discovery server must be reachable by all clients:

- Use `--network host` for host networking
- Use custom Docker networks for container-to-container communication
- Use `host.docker.internal` to reach host from container

## Security Considerations

- Containers run as non-root user `keykammer`
- CA certificates included for HTTPS discovery servers
- Mount keyfiles as read-only: `-v /path/to/keyfile:/app/keyfile:ro`
- Keep keyfiles secure on the host system

## Troubleshooting

### Container Won't Start

Check logs:
```bash
docker logs <container-id>
```

### Can't Connect to Discovery Server

- Verify network connectivity between containers
- Check if discovery server is running: `docker ps`
- Test with curl: `curl http://discovery:53952/health`

### TUI Issues

- Ensure interactive terminal: `docker run -it`
- Use `docker attach` to connect to running containers
- Detach with `Ctrl+P Ctrl+Q` (don't close terminal)

## Production Deployment

For production use:

1. Use specific version tags instead of `latest`
2. Set up proper logging with `--log-driver`
3. Use secrets management for keyfiles
4. Configure reverse proxy if needed
5. Set up monitoring and health checks

Example production run:
```bash
docker run -d \
  --name keykammer-discovery \
  --restart unless-stopped \
  -p 53952:53952 \
  keykammer:0.3.0-alpha \
  -discovery-server-mode
```
