# Super Mario Maker replacement server

Standalone open-source packaging of the Super Mario Maker NEX implementation used by Protarium. It supports the Wii U authentication and secure endpoints, Course World DataStore operations, course uploads and ratings, course records, buffer queues, the 100 Mario Challenge pickup HTTP endpoint, and an optional course administration interface.

## Preservation status

The protocol handlers and database implementation come from the Mario Maker components of the active multi-game `wsc-server` deployment. They include the August 2026 Course World and 100 Mario work. This repository separates those components into a buildable standalone process; consequently, its standalone binary is not claimed to be byte-for-byte identical to the production monolith.

The shared NEX protocol fork used by that deployment is included in `nex-protocols-common-go-patch/` because the login and secure-registration behavior depends on it. Production addresses, credentials, console certificates, database contents, uploaded courses, logs, and packet captures are excluded.

## Features

- Wii U NEX authentication and secure servers
- Course upload/download through PostgreSQL and S3-compatible storage
- Course World browsing, rankings, stars, records, and buffer queues
- 100 Mario Challenge pickup pools over HTTP
- Optional authenticated course import and moderation panel
- Placeholder metadata object for an empty Event Courses section

## Build and test

Go 1.25 or newer is required.

```bash
go test ./...
go build -o build/super-mario-maker .
```

Copy `.env.example` to `.env`, replace every placeholder, and never commit that file. The account service issuing NEX tokens must derive passwords with the same `PN_NEX_PASSWORD_SECRET` and encrypt login data with the same `PN_NEX_TOKEN_AES_KEY`.

The UDP authentication and secure ports must be reachable by consoles. Put TLS termination in front of `PN_SMM_PICKUP_LISTEN` for the HTTP pickup endpoint. Legacy Wii U TLS compatibility is an infrastructure concern and is intentionally not bundled here.

## Configuration

The complete safe template is in `.env.example`. Required settings cover PostgreSQL, the public NEX host and ports, S3-compatible storage, and the two NEX cryptographic secrets. `PN_SMM_ADMIN_PASSWORD` is optional; leaving it empty disables the administration interface.

## License

GNU Affero General Public License v3.0. See `LICENSE`. The bundled protocol fork retains its own license and notices.

This independent preservation and interoperability project is not affiliated with or endorsed by Nintendo.

_Deployed and maintained as part of the [Protarium Network](https://github.com/Protarium-Network) Wii U online service revival project._
