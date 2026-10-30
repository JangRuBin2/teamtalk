// TeamTalk relay: forwards game requests to the Anthropic API with the owner's
// key, so players only hold an access code.
//
// Secrets / vars (see relay/README.md):
//   ANTHROPIC_API_KEY  secret, the owner's API key
//   ACCESS_TOKENS      secret, comma-separated access codes, e.g. "minsu-7f3a,jiyoung-91bc"
//   ALLOWED_MODELS     var, comma-separated model ids
//   MAX_TOKENS         var, cap for max_tokens per request
//   DAILY_LIMIT        var, requests per access code per day (needs the USAGE KV binding)

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

export default {
  async fetch(req, env) {
    const url = new URL(req.url);
    if (req.method !== "POST" || url.pathname !== "/v1/messages") {
      return apiError(404, "not_found_error", "not found");
    }

    const token = req.headers.get("x-teamtalk-token") || "";
    if (!list(env.ACCESS_TOKENS).includes(token)) {
      return apiError(401, "authentication_error", "invalid access code");
    }

    let body;
    try {
      body = await req.json();
    } catch {
      return apiError(400, "invalid_request_error", "invalid JSON body");
    }
    if (!list(env.ALLOWED_MODELS || "claude-opus-5-5").includes(body.model)) {
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
