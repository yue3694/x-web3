# Bootstrapping the API & Worker

The Go modules in `apps/api` and `apps/worker` are **not pre-fetched** — `go mod tidy` must run once before the first build. This is intentional: it keeps the monorepo small and avoids pre-resolving transitive deps.

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Go | 1.24+ | `brew install go@1.24` / <https://go.dev/dl/> |
| PostgreSQL | 16+ | `brew install postgresql@16 && brew services start postgresql@16` |
| golang-migrate | latest | `brew install golang-migrate` or `go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest` |
| Docker (可选) | any | for redis + anvil in `deploy/docker-compose.yml` |
| Node | 20+ | for monorepo tooling |
| pnpm | 10.19 | `corepack enable && corepack prepare pnpm@10.19.0 --activate` |

## One-time setup

```bash
# 1. 启动 PostgreSQL（假设 brew）
brew services start postgresql@16

# 创建用户与数据库（首次；以后可跳过）
#   -s        superuser（本地 dev 省事）
#   --pwprompt 提示输入密码（务必与 .env 里 DATABASE_URL 一致；默认 xweb3）
createuser -s --pwprompt xweb3
createdb -O xweb3 xweb3
createdb -O xweb3 xweb3_test

# 备注：如果你是用 sudo -u postgres 创建的、或者 pg_hba.conf 走 peer 鉴权，
# 那可能根本不要密码，DATABASE_URL 里就把 :xweb3 删掉：
#   postgres://xweb3@localhost:5432/xweb3?sslmode=disable

# 2. 起 redis + anvil（可选；可用 brew 替代 redis，anvil 需 foundry）
docker compose -f deploy/docker-compose.yml up -d redis anvil

# 3. 复制并填写 .env（默认已配好本地 postgres + Privy dev stub）
cp .env.example .env
# 如需真 Privy：填 PRIVY_APP_ID / PRIVY_JWKS_URL / PRIVY_AUDIENCE，并设 PRIVY_DEV_STUB=

# 4. 拉 Go modules（首次必须）
cd apps/api  && go mod tidy
cd ../worker && go mod tidy
cd ../..

# 5. 跑 migrations + seed（连接串可直接传，也可走 .env）
./database/migrate.sh up --database "postgres://xweb3@localhost:5432/xweb3?sslmode=disable"
psql "postgres://xweb3@localhost:5432/xweb3?sslmode=disable" -f database/seed/0001_roles.sql
```

## Run

```bash
# from monorepo root
pnpm dev            # web at http://localhost:5173
pnpm dev:api        # = cd apps/api  && go run ./cmd/api   → :8080
pnpm dev:worker     # = cd apps/worker && go run ./cmd/worker
pnpm dev:stack      # 起 redis + anvil + api（postgres 走本地）
```

## Verification (M0 smoke)

```bash
# Health
curl http://localhost:8080/healthz
# → {"status":"ok"}

# Privy stub login（默认 PRIVY_DEV_STUB=1 + SUBJECT=did:privy:dev-1）
curl -i -X POST http://localhost:8080/api/v1/auth/privy/session \
  -H 'Content-Type: application/json' \
  -d '{"privyAccessToken":"stub"}'
# → 200 + Set-Cookie sid=...
```

## 环境变量加载机制

`apps/api` 启动时通过 [github.com/joho/godotenv](https://github.com/joho/godotenv) 自动加载 `.env`：

- 查找路径：`./.env` → `../.env` → ... 向上最多 6 层（命中 `.git`/`go.work` 同级即可），同时从源码位置向上找（go run / IDE run 兜底）
- **优先级**：`.env` 覆盖 shell 残留的 env（`godotenv.Overload`，dev 友好）
- 命令行 prefix `KEY=val ./api` 仍胜于 `.env`（go 在 process 启动时读 env，godotenv 之后才跑）
- prod 找不到 `.env` 是 no-op（K8s Secret / 真实 env 直接生效）

所以本地 dev 流程：

```bash
cp .env.example .env       # 改 DATABASE_URL / SESSION_SECRET 等
pnpm dev:api               # 自动读仓库根 .env，无需再 source / export
# 即使之前 shell 里 export 过老 DATABASE_URL，也会被 .env 覆盖
```

## Note on go.sum

`go.sum` is **not committed** to avoid drift. CI calls `go mod download` first; local dev runs `go mod tidy` once after cloning. If you change a dep, run `go mod tidy` and commit both `go.mod` and `go.sum`.