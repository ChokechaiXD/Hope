import json, os, urllib.request

tok_path = os.path.join(os.environ["LOCALAPPDATA"], "hermes", "cortex.json")
with open(tok_path) as f:
    data = json.load(f)
# Hermes connector config uses a flat "token" field.
tok = data.get("token", "") if isinstance(data, dict) else ""
print("tok_len", len(tok))

BASE = "http://127.0.0.1:7777"

def recall(text, key):
    body = json.dumps({"text": text, "include_candidates": True, "limit": 3}).encode()
    req = urllib.request.Request(BASE + "/v1/recalls", data=body, method="POST",
        headers={"Authorization": "Bearer " + tok, "Idempotency-Key": key, "Content-Type": "application/json"})
    try:
        resp = urllib.request.urlopen(req, timeout=5)
        d = json.load(resp)
    except Exception as e:
        print("ERR", key, e)
        return
    print(f"--- {key}: '{text}' -> items={len(d.get('items', []))}")
    for i in d.get("items", []):
        print("   ", round(i["score"], 3), i["memory"]["memory_key"][:55])

recall("curator automatic mode", "audit-k1")
recall("auto promote stale knowledge without manual review", "audit-k2")
recall("embedding semantic search vector", "audit-k3")
