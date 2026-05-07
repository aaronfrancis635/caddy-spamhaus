# caddy-spamhaus

A caddy module to add support for Spamhaus DROP lists in the new JSON format.

## Modules

| Module ID | Use |
|---|---|
| `http.matchers.spamhaus_drop` | Match inbound requests by remote IP |
| `http.ip_sources.spamhaus_drop` | IP range source for `trusted_proxies` |

The list is fetched once at startup then refreshed in the background (default every 24 h). If a refresh fails the previously cached list continues to be used.

---

## Installation

Using [`xcaddy`](https://github.com/caddyserver/xcaddy):

```sh
xcaddy build --with github.com/aaronfrancis635/caddy-spamhaus
```

---

## Configuration

Both modules share the same options:

| Field              | Type     | Default                                      | Description                          |
|--------------------|----------|----------------------------------------------|--------------------------------------|
| `url`              | `string` | `https://www.spamhaus.org/drop/drop_v4.json` | URL of the Spamhaus JSON DROP list.  |
| `refresh_interval` | `string` | `24h`                                        | How often to refresh the list.       |

**Caddyfile syntax (both modules):**

```caddyfile
# Minimal
spamhaus_drop

# Inline URL
spamhaus_drop https://www.spamhaus.org/drop/drop_v6.json

# Block with all options
spamhaus_drop {
    url              https://www.spamhaus.org/drop/drop_v4.json
    refresh_interval 6h
}
```

---

## Usage examples

### Block inbound requests from Spamhaus DROP IPs (Caddyfile)

Uses `http.matchers.spamhaus_drop`:

```caddyfile
example.com {
    @blocked spamhaus_drop
    abort @blocked

    respond "Hello, world!"
}
```

With options:

```caddyfile
example.com {
    @blocked spamhaus_drop {
        url              https://www.spamhaus.org/drop/drop_v4.json
        refresh_interval 6h
    }
    abort @blocked

    respond "Hello, world!"
}
```

### `trusted_proxies` (Caddyfile)
> I don't know why you'd want this, but it works I guess

Uses `http.ip_sources.spamhaus_drop`:

```caddyfile
{
    servers {
        trusted_proxies spamhaus_drop
    }
}
```

With options:

```caddyfile
{
    servers {
        trusted_proxies spamhaus_drop {
            url              https://www.spamhaus.org/drop/drop_v4.json
            refresh_interval 6h
        }
    }
}
```

---

## Available DROP lists

| Description         | URL                                              |
|---------------------|--------------------------------------------------|
| IPv4 DROP (default) | `https://www.spamhaus.org/drop/drop_v4.json`     |
| IPv6 DROP           | `https://www.spamhaus.org/drop/drop_v6.json`     |
