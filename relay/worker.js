// TeamTalk relay: forwards game requests to the Anthropic API with the owner's
// key, so players only hold an access code.
//
// Secrets / vars (see relay/README.md):
//   ANTHROPIC_API_KEY  secret, the owner's API key
//   ACCESS_TOKENS      secret, comma-separated access codes, e.g. "minsu-7f3a,jiyoung-91bc"
//   ALLOWED_MODELS     var, comma-separated model ids
//   MAX_TOKENS         var, cap for max_tokens per request
//   DAILY_LIMIT        var, requests per access code per day (needs the USAGE KV binding)
//   ROOMS              Durable Object binding for co-op rooms
//
// Co-op rooms: the host opens a WebSocket to /rooms/new (with an access code)
// and gets an invite code; guests connect to /rooms/<code> with no credentials.
// The room only forwards messages; the host's game runs the case.

const ANTHROPIC_URL = "https://api.anthropic.com/v1/messages";

function apiError(status, type, message) {
  return new Response(JSON.stringify({ type: "error", error: { type, message } }), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function list(v) {
  return (v || "").split(",").map((s) => s.trim()).filter(Boolean);
}

const ROOM_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789";
const MAX_GUESTS = 8;

function roomCode() {
  const b = crypto.getRandomValues(new Uint8Array(6));
  return Array.from(b, (x) => ROOM_ALPHABET[x % ROOM_ALPHABET.length]).join("");
}

function validToken(req, env) {
  return list(env.ACCESS_TOKENS).includes(req.headers.get("x-teamtalk-token") || "");
}

async function rooms(req, env, path) {
  if (req.headers.get("Upgrade") !== "websocket") {
    return new Response("expected websocket", { status: 426 });
  }
  if (path === "new") {
    if (!validToken(req, env)) return new Response("invalid access code", { status: 401 });
    for (let i = 0; i < 5; i++) {
      const code = roomCode();
      const stub = env.ROOMS.get(env.ROOMS.idFromName(code));
      const res = await stub.fetch(`https://room/?role=host&code=${code}`, req);
      if (res.status !== 409) return res;
    }
    return new Response("could not allocate a room", { status: 503 });
  }
  const code = path.toUpperCase();
  if (!/^[A-Z0-9]{6}$/.test(code)) return new Response("room not found", { status: 404 });
  return env.ROOMS.get(env.ROOMS.idFromName(code)).fetch("https://room/?role=guest", req);
}

export class Room {
  constructor(ctx) {
    this.ctx = ctx;
  }

  async fetch(req) {
    const url = new URL(req.url);
    const host = this.ctx.getWebSockets("host")[0];
    const isHost = url.searchParams.get("role") === "host";
    if (isHost && host) return new Response("room exists", { status: 409 });
    if (!isHost && !host) return new Response("room not found", { status: 404 });
    if (!isHost && this.ctx.getWebSockets("guest").length >= MAX_GUESTS) {
      return new Response("room full", { status: 429 });
    }

    const [client, server] = Object.values(new WebSocketPair());
    if (isHost) {
      this.ctx.acceptWebSocket(server, ["host"]);
      server.send(JSON.stringify({ room: url.searchParams.get("code") }));
    } else {
      const id = crypto.randomUUID().slice(0, 8);
      this.ctx.acceptWebSocket(server, ["guest", "g:" + id]);
      server.serializeAttachment({ id });
      host.send(JSON.stringify({ c: id, open: true }));
    }
    return new Response(null, { status: 101, webSocket: client });
  }

  webSocketMessage(ws, data) {
    if (typeof data !== "string") return;
    if (this.ctx.getTags(ws).includes("host")) {
      let f;
      try {
        f = JSON.parse(data);
      } catch {
        return;
      }
      const guest = this.ctx.getWebSockets("g:" + f.c)[0];
      if (!guest) return;
      if (f.close) guest.close(1000, "closed");
      else guest.send(JSON.stringify(f.d) + "\n"); // guests read JSON lines
      return;
    }
    const host = this.ctx.getWebSockets("host")[0];
    if (!host) return ws.close(1011, "host left");
    let d;
    try {
      d = JSON.parse(data);
    } catch {
      return;
    }
    host.send(JSON.stringify({ c: ws.deserializeAttachment().id, d }));
  }

  webSocketClose(ws) {
    this.gone(ws);
  }

  webSocketError(ws) {
    this.gone(ws);
  }

  gone(ws) {
    if (this.ctx.getTags(ws).includes("host")) {
      for (const g of this.ctx.getWebSockets("guest")) g.close(1001, "host left");
    } else {
      const host = this.ctx.getWebSockets("host")[0];
      host?.send(JSON.stringify({ c: ws.deserializeAttachment().id, close: true }));
    }
    try {
      ws.close(1000, "bye");
    } catch {}
  }
}

export default {
  async fetch(req, env) {
    const url = new URL(req.url);
    if (url.pathname.startsWith("/rooms/")) {
      return rooms(req, env, url.pathname.slice("/rooms/".length));
    }
    if (req.method !== "POST" || url.pathname !== "/v1/messages") {
      return apiError(404, "not_found_error", "not found");
    }

    const token = req.headers.get("x-teamtalk-token") || "";
    if (!validToken(req, env)) {
      return apiError(401, "authentication_error", "invalid access code");
    }

    let body;
    try {
      body = await req.json();
    } catch {
      return apiError(400, "invalid_request_error", "invalid JSON body");
    }
    if (!list(env.ALLOWED_MODELS || "claude-opus-5-5,claude-haiku-5-5").includes(body.model)) {
      return apiError(403, "permission_error", `model ${body.model} is not allowed`);
    }
    // The games never use tools; dropping them keeps the relay a plain chat endpoint.
    delete body.tools;
    delete body.mcp_servers;
    delete body.container;
    body.max_tokens = Math.min(Number(body.max_tokens) || 1024, Number(env.MAX_TOKENS || 16000));

    if (env.USAGE) {
      const day = new Date().toISOString().slice(0, 10);
      const key = `${token}:${day}`;
      const used = Number((await env.USAGE.get(key)) || 0);
      if (used >= Number(env.DAILY_LIMIT || 300)) {
        return apiError(429, "rate_limit_error", "daily limit reached for this access code");
      }
      await env.USAGE.put(key, String(used + 1), { expirationTtl: 60 * 60 * 48 });
    }

    const headers = {
      "content-type": "application/json",
      "x-api-key": env.ANTHROPIC_API_KEY,
      "anthropic-version": req.headers.get("anthropic-version") || "2023-06-01",
    };
    const beta = req.headers.get("anthropic-beta");
    if (beta) headers["anthropic-beta"] = beta;

    const upstream = await fetch(ANTHROPIC_URL, { method: "POST", headers, body: JSON.stringify(body) });
    const out = new Headers({ "content-type": upstream.headers.get("content-type") || "application/json" });
    const retry = upstream.headers.get("retry-after");
    if (retry) out.set("retry-after", retry);
    return new Response(upstream.body, { status: upstream.status, headers: out });
  },
};
