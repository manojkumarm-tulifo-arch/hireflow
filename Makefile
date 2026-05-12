.PHONY: tidy build run test test-unit test-integration migrate-up migrate-down lint \
        db-up db-down db-reset db-logs db-psql

# Per-context migration tracking — each bounded context owns its schema and
# its own migration version table.
MIGRATE_AUTH      := migrate -path migrations/auth         -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_auth"
MIGRATE_INTENT    := migrate -path migrations/hiringintent -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_hiringintent"
MIGRATE_POSTING   := migrate -path migrations/jobposting   -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_jobposting"
MIGRATE_SOURCING  := migrate -path migrations/sourcing     -database "$(DATABASE_URL)&x-migrations-table=schema_migrations_sourcing"

tidy:
	go mod tidy

build:
	go build -o bin/api ./cmd/api

run:
	go run ./cmd/api

test:
	go test ./... -race -count=1

test-unit:
	go test ./internal/... -race -count=1

test-integration:
	go test ./tests/... -race -count=1 -tags=integration

migrate-up:
	$(MIGRATE_AUTH) up
	$(MIGRATE_INTENT) up
	$(MIGRATE_POSTING) up
	$(MIGRATE_SOURCING) up

migrate-down:
	$(MIGRATE_SOURCING) down 1
	$(MIGRATE_POSTING) down 1
	$(MIGRATE_INTENT) down 1
	$(MIGRATE_AUTH) down 1

lint:
	go vet ./...
	gofmt -l -s .

# ---- Docker Postgres (alternative to local Homebrew Postgres) ----
# When using these, set:
#   export DATABASE_URL="postgres://hireflow:hireflow@localhost:5433/hireflow?sslmode=disable"

db-up:
	docker compose up -d --wait

db-down:
	docker compose down

db-reset:
	docker compose down -v
	docker compose up -d --wait

db-logs:
	docker compose logs -f postgres

db-psql:
	docker compose exec postgres psql -U hireflow -d hireflow
