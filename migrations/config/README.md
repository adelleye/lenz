# Database migrations

Soda/Pop is the database migration runner for this repository:

From the repository root:

```sh
DATABASE_URL='postgres://lenzcore:lenzcore123@localhost:55432/lenzcore?sslmode=disable' \
  ./scripts/migrate.sh up
```

`scripts/migrate.sh down` applies one down migration by default. Pass Soda flags
after the command, for example `scripts/migrate.sh down --step 2`.

Reference: https://gobuffalo.io/documentation/database/soda/
