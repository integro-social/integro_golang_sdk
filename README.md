# Integro SDK — Go

Generated Go client for the [Integro](https://integro.social) API.

**Do not edit this repository.** Every file is machine-generated from the
Integro API definition and force-synced on every release; each sync commit
names the source revision. Pull requests cannot be accepted — every file is
replaced on the next sync — but issues are welcome here.

## Install

The module is named `integro_sdk` (not a URL path), so it is consumed through a
local `replace` — clone (or submodule) the repo next to your project:

```sh
git clone https://github.com/integro-social/integro_golang_sdk.git integro_sdk
go mod edit -require=integro_sdk@v0.0.0 -replace=integro_sdk=./integro_sdk
go mod tidy
```

## Quickstart

The default API host is `https://api.integro.social`.

```go
package main

import (
	"fmt"

	client "integro_sdk"
	"integro_sdk/routes/user"
)

func main() {
	c := client.NewClient("https://api.integro.social")
	c.SetBearer("integro_...") // API key from the dashboard

	// Requires `ViewUsers` — every route function's doc comment carries its
	// permission contract.
	n, err := user.Count(c)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%d users\n", n)
}
```

## Auth

Every request sends `Authorization: Bearer <token>`, where the token is an
Integro API key (`integro_...`, issued in the dashboard) or a user session
token. Set it with `SetBearer` (empty string clears it).

Inbound event webhooks are signed with `X-Integro-Signature`
(`sha256=<hex>`, HMAC-SHA256 over the raw body with the secret shown when the
webhook is configured).

## Layout

- `routes/` — one package per domain (`message`, `post`, `group`, ...); each
  function documents the endpoint and the exact permissions it requires.
- `types/` — request/response types mirroring the server's validated newtypes.
- `runtime.go` — the HTTP/SSE/WebSocket engine behind every route (root
  package `client`).

## Versioning

The module version mirrors the Integro API version at the source
revision named by the latest sync commit.
