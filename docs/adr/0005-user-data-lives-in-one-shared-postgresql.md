# User data lives in one shared PostgreSQL

summary: User accounts moved from a per-container SQLite file to a shared PostgreSQL database, because replicated logic instances cannot share a file baked into an image.

## Context

User accounts lived in `db/gochat.sqlite3`, a file copied into the image at build
time and mounted from nowhere. Every logic replica therefore had its own private
copy of the database.

That made the repository's headline claim false. `docker compose up --scale
logic=3` started three replicas that disagreed about which users existed: a user
registered against one replica could not log in against another, and which
replica served a request depended on rpcx's load balancing. Nothing failed
loudly — logins just failed intermittently.

## Decision

PostgreSQL, one instance, shared by every logic replica. Schema lives in
`db/migrations/*.sql`, applied by the postgres container on first start or by
`make db-migrate`. Connection settings come from `[common-db]`, overridable by
`DB_*` environment variables.

## Why

Shared mutable state has to live somewhere both replicas can reach; that is the
whole requirement, and any networked database satisfies it. PostgreSQL was chosen
over MySQL for no reason stronger than a preference — either would do.

Two details are deliberate:

**Uniqueness is a database constraint, not application code.** The previous
implementation read for an existing username and then inserted, which two
concurrent registrations can both pass. `idx_users_user_name` makes the database
reject the second, and the DAO maps the violation to `ErrUserNameTaken`.

**Migrations are SQL files, not GORM AutoMigrate.** AutoMigrate makes the schema
a function of whatever the struct looked like at deploy time, which is invisible
in review and unrepeatable across versions. A numbered SQL file is neither.

The table is `users` rather than `user` because `user` is reserved in PostgreSQL
and only works quoted.

## Alternatives

**Mount the SQLite file from a shared volume.** Works on one host, fails as soon
as replicas are on different machines, and SQLite's write locking would serialise
every registration anyway.

**Keep SQLite and pin logic to one replica.** Would mean withdrawing the scaling
claim, which is the point of the project.

## Consequences

- PostgreSQL is a hard startup dependency for logic. `db.Init` retries, since a
  healthcheck on the database does not guarantee readiness at first query.
- `db.Init` is called explicitly instead of from an `init` function, so importing
  the package no longer opens a connection as a side effect.
- The binary no longer needs cgo, since `lib/pq` is pure Go. The image dropped
  the sqlite3 package with it.
- Existing SQLite data is not migrated. It held development accounts only, and
  [0006](./0006-passwords-are-hashed-with-bcrypt.md) would have invalidated the
  rows regardless.
