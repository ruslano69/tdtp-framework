# Deployment Files

Docker Compose files for local development and testing.

## Files

- `docker-compose.example.yml` - Basic example setup
- `docker-compose.mssql.yml` - MS SQL Server with test data
- `docker-compose.yml` - Your local setup (gitignored)

## Quick Start

### PostgreSQL + RabbitMQ

```bash
cp deployments/docker-compose.example.yml deployments/docker-compose.yml
docker-compose -f deployments/docker-compose.yml up -d
```

### MS SQL Server

```bash
docker-compose -f deployments/docker-compose.mssql.yml up -d
```

## Services

- **PostgreSQL:** `localhost:5432` (user: postgres, password: postgres)
- **MS SQL:** `localhost:1433` (user: sa, password: YourStrong@Passw0rd)
- **RabbitMQ:** `localhost:5672` (user: guest, password: guest)
- **RabbitMQ Management:** `http://localhost:15672`

## Notes

- `docker-compose.yml` is gitignored - customize without committing secrets
- Use example files as templates
