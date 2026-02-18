# oas-cloud-go

Go cloud backend for OAS runtime linkage.

> Redis is mandatory in this backend. Service startup will fail when Redis is unreachable.

## Frontend framework

- Framework: **Vue 3 + Vite + Element Plus**
- Location: `frontend/`
- Includes full role consoles:
  - Super admin: bootstrap init/login, manager renewal key issuing, manager status management
  - Manager: register/login, redeem renewal key, activation code issuing, quick create user, user task/log management
  - User: register by activation code, account login, redeem code, self task/log management

## Quick start

```bash
go mod tidy
go run ./cmd/server
```

```bash
cd frontend
npm install
npm run dev
```

## Environment variables

- `ADDR` default `:8080`
- `DATABASE_URL` PostgreSQL DSN
- `REDIS_ADDR` default `127.0.0.1:6379`
- `REDIS_PASSWORD` default empty
- `REDIS_DB` default `0`
- `REDIS_KEY_PREFIX` default `oas:cloud`
- `JWT_SECRET` JWT signing secret
- `JWT_TTL` default `24h`
- `AGENT_JWT_TTL` default `12h`
- `USER_TOKEN_TTL` default `4320h`
- `DEFAULT_LEASE_SECONDS` default `90`
- `MAX_POLL_LIMIT` default `20`

## API prefix

All APIs are under `/api/v1`.

## One-command deployment example

`docker-compose.yml` is provided for integrated deployment:

- URL: `http://localhost:8088` (frontend)
- Backend API via nginx reverse proxy: `/api/v1/*`
- Backend direct health check: `http://localhost:8080/health`

```bash
docker compose up -d --build
```
