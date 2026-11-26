---
name: deeplink-campaign-links
description: Create tracked short links with social preview cards and read their click analytics through a self-hosted deeplink server. Use when the user wants short links for a launch, post or campaign, one link per channel for attribution, link expiry, or a report on how their links performed.
---

# Campaign links with deeplink

[deeplink](https://github.com/yinebebt/deeplink) is an open-source, self-hosted short link server (Go, MIT). It gives each link an Open Graph preview card, optional expiry, and click analytics broken down by platform, referrer and day. It exposes these as MCP tools, so you work with links directly instead of through curl.

## 1. Check the connection

Look for the deeplink MCP tools: `create_link`, `get_link`, `list_links`, `link_stats`, `delete_link`, `list_types`. Call `list_types` once to confirm the server answers.

If the tools are missing, the user needs a running server. Walk them through it, then stop and ask them to restart their agent:

```bash
git clone https://github.com/yinebebt/deeplink && cd deeplink
DEEPLINK_API_KEY=<long-random-key> \
DEEPLINK_BASE_URL=https://<their-domain> \
go run ./cmd/deeplink            # SQLite by default, listens on :8090
```

A `Dockerfile` is in the repo for deploying it anywhere that runs containers. `DEEPLINK_BASE_URL` must be the public URL the short links will use. Then connect the agent:

```bash
# Claude Code
claude mcp add --transport http deeplink https://<their-domain>/mcp \
  --header "Authorization: Bearer <key>"
```

Other MCP clients (Cursor, Codex, etc.): add an HTTP MCP server at `https://<their-domain>/mcp` with the header `Authorization: Bearer <key>`.

Never ask the user to paste their API key into the chat. If they already did, tell them to rotate it with `deeplink keys rotate`.

## 2. Create links

When the user wants links for something they're promoting:

1. Get the destination URL. If you can fetch it, read its title and description to draft the preview card; otherwise ask.
2. Ask which channels it's going to (e.g. X, LinkedIn, newsletter, Discord). Default to one link per channel. Referrer data alone is unreliable: apps, email clients and DMs often send none, so a separate link per channel is the only clean attribution.
3. Call `create_link` for each channel with:
   - `url`: the destination. If the user wants UTM tags, append them here (`utm_source=<channel>&utm_campaign=<name>`).
   - `title`, `description`: the preview card. Title under ~60 characters, description under ~155. Write for the person scrolling past, not for SEO.
   - `image_url`: only if the user has one; PNG, JPG or WebP, ideally 1200×630.
   - `expires_at`: RFC 3339, only for time-limited offers or events. It must be in the future.
4. Return a table: channel, short URL, expiry. Keep the short IDs; they are what the stats call needs.

## 3. Report performance

When asked how links did:

1. `list_links` to find them (match on title or destination if the user doesn't give IDs).
2. `link_stats` for each relevant link.
3. Report:
   - Total clicks per link, ranked.
   - Platform split (android / ios / web) — useful for deciding where an app link or store badge matters.
   - Top referrers, noting that missing referrers usually mean apps or email, not "direct".
   - Daily trend: when clicks peaked and whether it has died off.
4. End with one or two concrete takeaways (which channel to double down on, whether a link is still getting traffic). Don't pad it.

Click counts are flushed in the background, so a click from the last second or two may not show yet.

## 4. Clean up

`delete_link` stops a link resolving immediately; the record is purged after a grace period. Confirm with the user before deleting, and never bulk-delete without listing what will go.

## Limits

- Requires the user to run their own deeplink server; this skill does not host links.
- Analytics are clicks only: no conversions, no unique-visitor counts.
- `create_link` creates; editing a link's fields is done over the HTTP API (`PATCH /{shortID}`), not MCP.
