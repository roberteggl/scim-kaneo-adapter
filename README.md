# scim-kaneo-adapter

SCIM 2.0 bridge for [Kaneo](https://github.com/usekaneo/kaneo). An identity
provider (e.g. [authentik](https://goauthentik.io/)) talks SCIM to this adapter;
the adapter keeps the SCIM user/group catalogue and translates membership into
Kaneo workspace invites via Kaneo's Better Auth organization API (`x-api-key`).

Unlike a 1:1 group sync, workspace access is driven by an **assignment matrix**:
each SCIM group `displayName` maps to one or more `(workspace, role)` pairs.
When a user belongs to several mapped groups for the same workspace, the
**highest role wins** (`admin` > `member` > `viewer`). Owner is never assigned.

## How it works

1. The IdP provisions SCIM Users and Groups (`/scim/v2/...`).
2. The adapter stores that catalogue in memory (optionally persisted to
   `STORE_FILE`).
3. After membership-affecting mutations, it recomputes desired Kaneo roles from
   `ASSIGNMENTS_FILE` and invites / updates / removes the user in each
   referenced workspace.

Users must already exist in Kaneo (typically via OIDC). The adapter invites by
email; if Kaneo has no matching account yet, invite behaviour follows Kaneo's
API (email invite / pending membership).

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `LISTEN_ADDR` | no | `:8080` | HTTP bind address. |
| `SCIM_TOKEN` | **yes** | — | Bearer token the SCIM client must present (constant-time compare). |
| `KANEO_URL` | **yes** | — | Kaneo origin, e.g. `https://kaneo.example.com`. |
| `KANEO_API_KEY` | **yes** | — | Kaneo admin API key (`x-api-key`). |
| `ASSIGNMENTS_FILE` | **yes** | — | Path to YAML assignment matrix (see below). |
| `STORE_FILE` | no | — | JSON persistence for SCIM Users/Groups. Unset = memory only. |

### Assignments YAML

```yaml
assignments:
  - group: kaneo-neuland-admin
    workspace: neuland   # slug or organization id
    role: admin          # viewer | member | admin
  - group: kaneo-neuland-member
    workspace: neuland
    role: member
```

See [`examples/assignments.yaml`](examples/assignments.yaml).

## SCIM surface

All routes live under `/scim/v2` and require `Authorization: Bearer <SCIM_TOKEN>`.
Content type: `application/scim+json` (JSON also accepted on request).

- `GET /ServiceProviderConfig`, `/Schemas`, `/ResourceTypes`
- Users: `POST` / `GET` / `GET {id}` / `PUT` / `PATCH` / `DELETE`
- Groups: `POST` / `GET` / `GET {id}` / `PUT` / `PATCH` / `DELETE`

Filters support equality on common attributes (`userName`, `emails.value`,
`externalId`, `displayName`) plus `startIndex`/`count` pagination where
applicable.

## Run locally

```sh
export SCIM_TOKEN=dev-token
export KANEO_URL=http://127.0.0.1:3000
export KANEO_API_KEY=...
export ASSIGNMENTS_FILE=./examples/assignments.yaml

go run ./cmd/scim-kaneo-adapter
```

## Container image

CI publishes multi-arch images to GHCR:

```text
ghcr.io/roberteggl/scim-kaneo-adapter:latest
ghcr.io/roberteggl/scim-kaneo-adapter:sha-<gitsha>
ghcr.io/roberteggl/scim-kaneo-adapter:1.0.0   # from tag v1.0.0
```

```sh
docker run --rm -p 8080:8080 \
  -e SCIM_TOKEN=... \
  -e KANEO_URL=https://kaneo.example.com \
  -e KANEO_API_KEY=... \
  -e ASSIGNMENTS_FILE=/config/assignments.yaml \
  -v "$PWD/examples/assignments.yaml:/config/assignments.yaml:ro" \
  ghcr.io/roberteggl/scim-kaneo-adapter:latest
```

The image runs as UID `10001` from `scratch` (CA certs + tzdata included). Mount
a writable volume at `/data` if you set `STORE_FILE=/data/store.json`.

## Development

```sh
go test ./...
gofmt -w .
```

## Licence

MIT — see [LICENSE](LICENSE).
