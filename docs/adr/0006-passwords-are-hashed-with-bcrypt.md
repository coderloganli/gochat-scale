# Passwords are hashed with bcrypt

summary: Passwords are stored as bcrypt hashes at the default cost, and login spends the same time on an unknown user as on a wrong password.

## Context

Passwords were stored as the user typed them and login compared them with `!=`.
Anyone reading the database — a backup, a dump, a log of a query — read every
password, and because people reuse passwords the damage would not stop at this
system.

## Decision

`pkg/password` wraps bcrypt. `Hash` on registration, `Verify` on login, nothing
else touches the stored value.

Two details beyond the obvious:

**A dummy comparison on the unknown-user branch.** Without it, a login for a
nonexistent user returns in microseconds while a wrong password takes ~70 ms,
which turns response time into a working account enumeration oracle.
`VerifyDummy` spends the same time so the two branches are indistinguishable.

**`GOCHAT_BCRYPT_COST` can lower the cost factor.** Only for load tests: at the
default cost the KDF dominates the login endpoint and the measurement stops
describing the service. Lowering it warns in the log, and must not happen
anywhere else.

## Why

bcrypt is deliberately slow, salts each hash individually, and carries its cost
factor in the output so it can be raised later without a schema change. The
default cost of 10 is roughly 50–100 ms, the standard trade of login latency
against offline cracking speed.

Argon2id is the better modern answer and would have been a defensible choice.
bcrypt won on `golang.org/x/crypto` already being an indirect dependency, a
well-known constant-time comparison, and no tuning parameters to get wrong. The
gap between the two matters far less than the gap between either and plaintext.

## Alternatives

**Argon2id or scrypt.** Better resistance to GPU and ASIC attacks. Worth
revisiting; three parameters to choose rather than one.

**SHA-256 with a salt.** Fast by design, which is exactly wrong for passwords.

**Migrate existing plaintext rows by hashing them in place.** Rejected. It would
mean writing code that reads plaintext passwords, and the affected rows were
development accounts.

## Consequences

- Existing plaintext rows can no longer authenticate. A plaintext value is not a
  valid bcrypt hash, and `Verify` rejects it — the intended outcome, and covered
  by a test.
- Login and registration got ~70 ms slower. Every capacity number measured before
  this change overstates the login path; see `docs/benchmarks.md`.
- Passwords longer than 72 bytes are rejected rather than silently truncated,
  which is what bcrypt would otherwise do.
- The password column is `varchar(255)` although a bcrypt hash is 60 characters,
  leaving room to change algorithm without a migration.
