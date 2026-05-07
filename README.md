# caddy-spamhaus

A caddy module to add support for Spamhaus DROP lists in the new JSON format.

---

## Installation

Using [`xcaddy`](https://github.com/caddyserver/xcaddy):

```sh
$ xcaddy build --with github.com/aaronfrancis635/caddy-spamhaus
```

---

## Configuration

### Module ID

```
http.ip_sources.spamhaus_drop
```

### Config


| Field              | Type     | Default                                          | Description                                   |
|--------------------|----------|--------------------------------------------------|-----------------------------------------------|
| `url`              | `string` | `https://www.spamhaus.org/drop/drop_v4.json`     | URL of the Spamhaus JSON DROP list to fetch.  |
| `refresh_interval` | `string` | `24h`                                            | How often to refresh the list   |

### Caddyfile

The module can be used anywhere Caddy accepts an `ip_sources` block, for
example inside a `remote_ip` matcher or as `trusted_proxies` (if you for some reason want that?).

**Minimal (all defaults):**

```caddyfile
spamhaus_drop
```

**With an inline URL:**

```caddyfile
spamhaus_drop https://www.spamhaus.org/drop/drop_v6.json
```

**With a block (all options):**

```caddyfile
spamhaus_drop {
    url              https://www.spamhaus.org/drop/drop_v4.json
    refresh_interval 6h
}
```

---

## Usage examples

### Block requests from Spamhaus DROP IPs

```caddyfile
example.com {
    @blocked remote_ip {
        ranges {
            source spamhaus_drop
        }
    }
    abort @blocked

    respond "Hello, world!"
}
```

### Combine with other IP sources

```caddyfile
example.com {
    @blocked remote_ip {
        ranges {
            source spamhaus_drop
            source static 10.0.0.0/8
        }
    }
    abort @blocked
}
```

### Use as `trusted_proxies`

```caddyfile
{
    servers {
        trusted_proxies spamhaus_drop
    }
}
```

---

## Available DROP lists

| Description          | URL                                                         |
|----------------------|-------------------------------------------------------------|
| IPv4 DROP (default)  | `https://www.spamhaus.org/drop/drop_v4.json`                |
| IPv6 DROP            | `https://www.spamhaus.org/drop/drop_v6.json`                |


---

