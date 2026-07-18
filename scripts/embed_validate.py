import json, os, sqlite3, struct, math, re

p = os.path.join(os.environ["LOCALAPPDATA"], "Cortex", "cortex.db")
c = sqlite3.connect(p)

def decode(blob):
    n = len(blob)//8
    return [struct.unpack_from("<d", blob, i*8)[0] for i in range(n)]

def cos(a, b):
    dot = sum(x*y for x,y in zip(a,b))
    return dot if dot>0 else 0.0

# load all memories with embeddings + content
rows = c.execute("""SELECT m.id, m.memory_key, r.title, r.content, m.embedding
FROM memories m JOIN memory_revisions r ON r.memory_id=m.id AND r.revision=m.current_revision
WHERE length(m.embedding)>0""").fetchall()
print("loaded", len(rows), "memories")

# replicate offline embedder (must match Go embedText)
import hashlib
EMBED_DIM = 256
def fold(s):
    out=[]
    for r in s:
        if 0xFF01 <= ord(r) <= 0xFF5E: r=chr(ord(r)-0xFF01+0x21)
        if unicodedata.combining(r): continue
        out.append(r)
    return "".join(out)
import unicodedata
def tokens(text):
    norm = fold(text.lower())
    parts = re.split(r'[^a-z0-9#ก-๛]+', norm)
    out=[]
    for pk in parts:
        if not pk: continue
        out.append(pk)
        if len(pk)>4 and any(0x0E00<=ord(ch)<=0x0E7F for ch in pk):
            rs=list(pk)
            for i in range(0,len(rs)-1): out.append("".join(rs[i:i+2]))
    if not out: out=["<empty>"]
    return out
def hashfeat(vec, tok, w):
    h=hashlib.sha256(tok.encode()).digest()
    prim=int.from_bytes(h[0:4],"big")%EMBED_DIM
    sec=int.from_bytes(h[4:8],"big")%EMBED_DIM
    sign=-1.0 if h[8]%2==1 else 1.0
    vec[prim]+=w*sign; vec[sec]+=w*0.5*sign
def embed(text):
    vec=[0.0]*EMBED_DIM
    toks=tokens(text)
    for t in toks: hashfeat(vec,t,1.0)
    for i in range(1,len(toks)): hashfeat(vec,toks[i-1]+" "+toks[i],0.5)
    for t in toks:
        if t.startswith("#") and len(t)>1: hashfeat(vec,t,1.0)
    norm=math.sqrt(sum(v*v for v in vec))
    if norm>0:
        vec=[v/norm for v in vec]
    return vec

def query(q):
    qv=embed(q)
    scored=[]
    for mid,key,title,content,blob in rows:
        vec=decode(blob)
        if len(vec)!=EMBED_DIM: continue
        scored.append((cos(qv,vec),key,title))
    scored.sort(reverse=True)
    return scored[:5]

for q in ["curator automatic mode","auto promote stale knowledge without manual review","embedding semantic vector search","how to deploy hermes skill"]:
    print(f"\n=== query: {q}")
    for sc,key,title in query(q):
        print(f"  {sc:.3f}  {key[:50]}")
c.close()
