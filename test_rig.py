#!/usr/bin/env python3
"""Fake share publisher + NNTP server for exercising usnfetch end to end.

Builds a share in the real wire format:
  * file split into SEGMENT_SIZE slices, AES-256-GCM(folder_key), bare ciphertext yEnc'd
  * msgid_pool interleaves primary {m,n,t,s} and redundancy {m,ref} (stride 2)
  * core index JSON (seg_range form) -> xz -> nonce||tag||ct envelope -> 2 part articles
  * v4 manifest (public access, Ed25519 owner key + fingerprint) as plain-JSON yEnc article
  * usn:// link
Options: explicit=True lists each file's pool indices instead of a seg_range; sharded=True
publishes a v5-style sharded index (one shard per file, shard-local indices + digests);
ragged=True varies the segment sizes (exercises the per-segment offset table).
pack=True packs files under PACK_THRESHOLD into shared articles like the publisher does (binary
header v3; every other member gzip/zstd-compressed with flag bit 0; the second pack gzip'd as a
whole; the first pack's primary article missing so its redundancy copy is used). Packed files
are listed in files[] with no segments plus packed_segments[]; sharded puts the packs in their
own shard with shard-local pool indices.
Faults injected (faults=True):
  * segment FAULT_PRIMARY_MISSING: primary article 430, copy present
  * segment FAULT_PRIMARY_CORRUPT: primary article has a flipped byte (GCM tag fails), copy present
  * segment FAULT_ALL_MISSING: both copies 430 until server.release_missing() is called
"""
import base64, gzip, hashlib, json, lzma, os, secrets, socket, struct, threading
import zstandard
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization
import sabctools

SEGMENT_SIZE = 700000
PACK_THRESHOLD = 500_000
FAULT_PRIMARY_MISSING, FAULT_PRIMARY_CORRUPT, FAULT_ALL_MISSING = 5, 9, 17

def rand_msgid():
    a = base64.b64encode(secrets.token_bytes(16)).decode().rstrip("=").replace("+", "a").replace("/", "b")
    return f"{a[:22]}@{secrets.token_hex(5)}.{secrets.token_hex(2)}"

def yenc_article(data: bytes, name="x") -> bytes:
    body, crc = sabctools.yenc_encode(data)  # already wrapped at 128 with CRLF
    # dot-stuff lines starting with '.' (NNTP transparency), as a real server would send
    lines = [(b"." + l if l.startswith(b".") else l) for l in body.split(b"\r\n")]
    head = b"=ybegin part=1 total=1 line=128 size=%d name=%s\r\n=ypart begin=1 end=%d\r\n" % (len(data), name.encode(), len(data))
    tail = b"\r\n=yend size=%d part=1 pcrc32=%08x\r\n" % (len(data), crc)
    return head + b"\r\n".join(lines) + tail

def build_share(plain: bytes, workdir: str):
    return build_share_files([("images/test.img", plain)], workdir)

# ---- publisher-side signing (v4 tbs 1 / v5 tbs 1 layouts) ----
def _lp(b): return len(b).to_bytes(4, "big") + b

def shards_root(shards):
    acc, n = bytearray(32), 0
    for s in shards:
        tok = hashlib.sha256(int(s["shard_index"]).to_bytes(4, "big") + bytes.fromhex(s["digest"])).digest()
        acc = bytearray(a ^ b for a, b in zip(acc, tok)); n += 1
    return hashlib.sha256(b"USSSR1\x00" + n.to_bytes(8, "big") + bytes(acc)).hexdigest()

def sign_manifest(m, priv, pub_raw):
    adig = hashlib.sha256(json.dumps(m["access"], separators=(",", ":"), sort_keys=True).encode()).digest()
    head = (_lp(pub_raw) + _lp(m["share_id"].encode()) + _lp(m["folder_id"].encode()) + m["folder_version"].to_bytes(4, "big")
            + _lp(m["prev_version_message_id"].encode()) + _lp(m["created_at"].encode()) + b"\x00" + adig)
    if m["format"] == "core_index":
        tbs = (b"usenetsync:manifest-sig:v1" + b"\x05\x01" + head + bytes.fromhex(m["shards_root"]) + m["num_shards"].to_bytes(4, "big")
               + _lp(m["shard_key"].encode()) + _lp(m["folder_digest"].encode()))
        extra = {"shards_root": m["shards_root"]}
    else:
        enc = len(m["core_index"]["parts"]).to_bytes(4, "big")
        for p in sorted(m["core_index"]["parts"], key=lambda p: p["part_number"]):
            enc += p["part_number"].to_bytes(4, "big") + _lp(p["primary_msgid"].encode()) + len(p["redundancy_msgids"]).to_bytes(4, "big")
            enc += b"".join(_lp(r.encode()) for r in p["redundancy_msgids"]) + p["size"].to_bytes(8, "big") + b"\x00"
        tbs = b"usenetsync:manifest-sig:v1" + b"\x04\x01" + head + enc
        extra = {"core_index_root": hashlib.sha256(enc).hexdigest()}
    m["sig"] = {"alg": "ed25519", "tbs_version": 1, "access_digest": adig.hex(), **extra,
                "signature": base64.b64encode(priv.sign(tbs)).decode()}

def build_packs(small, aes, articles, cap=200_000, primary_missing=True):
    """Pack (path, plaintext) pairs the way the official client's segmenter does. Returns the packs'
    shard-local pool, their packed_segments[] entries (local pool indices) and the files' records."""
    groups, cur, cur_size = [], [], 12
    for item in small:
        if cur and cur_size + 25 + len(item[1]) > cap:
            groups.append(cur); cur, cur_size = [], 12
        cur.append(item); cur_size += 25 + len(item[1])
    if cur:
        groups.append(cur)
    lpool, entries, recs = [], [], []
    for gi, grp in enumerate(groups):
        header, data, metas = bytearray(struct.pack("<II", 3, len(grp))), bytearray(), []
        for k, (path, plain) in enumerate(grp):
            fid = secrets.token_hex(16)
            blob, flags = plain, 0
            if k % 2 == 1 and plain:
                blob, flags = (zstandard.ZstdCompressor().compress(plain) if k % 4 == 1 else gzip.compress(plain)), 1
            header += struct.pack("<16sIIB", bytes.fromhex(fid), len(data), len(blob), flags)
            data += blob
            metas.append({"file_id": fid, "path": path, "name": path.rsplit("/", 1)[-1], "size": len(plain),
                          "hash": hashlib.sha256(plain).hexdigest()})
        final = struct.pack("<I", len(header)) + bytes(header) + bytes(data)
        if gi == 1:
            final = gzip.compress(final)                       # whole-segment compression
        nonce = secrets.token_bytes(12)
        ct_tag = aes.encrypt(nonce, final, None)
        ct, tag = ct_tag[:-16], ct_tag[-16:]
        prim, copy = rand_msgid(), rand_msgid()
        pidx = len(lpool)
        lpool.append({"m": prim, "n": nonce.hex(), "t": tag.hex(), "s": len(final)})
        lpool.append({"m": copy, "ref": pidx})
        if not (primary_missing and gi == 0):
            articles[prim] = yenc_article(ct)
        articles[copy] = yenc_article(ct)
        entries.append({"segment_id": secrets.token_hex(16), "index": gi, "msgid_pool_index": pidx,
                        "encryption_nonce": nonce.hex(), "encryption_tag": tag.hex(), "size": len(final),
                        "hash": hashlib.sha256(final).hexdigest(), "files": metas})
        recs += metas
    return lpool, entries, recs

def build_share_files(entries, workdir: str, explicit=False, sharded=False, ragged=False, faults=True, pack=False):
    """entries: [(share path, plaintext bytes)]. Fault indices count segments across all files."""
    key = secrets.token_bytes(32)
    aes = AESGCM(key)
    folder_id = secrets.token_hex(16)
    share_id = secrets.token_hex(16)
    articles = {}           # msgid -> yEnc article bytes
    held = {}               # FAULT_ALL_MISSING articles, released later
    pool = []               # global pool: what a v4 index and the consumer db hold
    files = []              # file records with global pool indices
    shards = []             # per file: (shard-local pool, file record with local indices)
    g = 0                   # global segment number
    small = [(p, d) for p, d in entries if pack and len(d) < PACK_THRESHOLD]
    entries = [(p, d) for p, d in entries if not (pack and len(d) < PACK_THRESHOLD)]
    for path, plain in entries:
        lpool, lsegs = [], []
        off = 0
        while off < len(plain):
            size = SEGMENT_SIZE - (len(lsegs) % 3) * 1000 if ragged else SEGMENT_SIZE
            pt = plain[off:off + size]; off += len(pt)
            nonce = secrets.token_bytes(12)
            ct_tag = aes.encrypt(nonce, pt, None)
            ct, tag = ct_tag[:-16], ct_tag[-16:]
            prim, copy = rand_msgid(), rand_msgid()
            pidx = len(lpool)
            lpool.append({"m": prim, "n": nonce.hex(), "t": tag.hex(), "s": len(pt)})
            lpool.append({"m": copy, "ref": pidx})
            lsegs.append(pidx)
            prim_ct = ct
            if faults and g == FAULT_PRIMARY_CORRUPT:
                b = bytearray(ct); b[min(1000, len(b) - 1)] ^= 0xFF; prim_ct = bytes(b)
            if not faults or g not in (FAULT_PRIMARY_MISSING, FAULT_ALL_MISSING):
                articles[prim] = yenc_article(prim_ct)
            if not faults or g != FAULT_ALL_MISSING:
                articles[copy] = yenc_article(ct)
            else:
                held[prim] = yenc_article(ct)
                held[copy] = yenc_article(ct)
            g += 1
        rec = {"file_id": "f" + secrets.token_hex(8), "path": path, "name": path.rsplit("/", 1)[-1],
               "size": len(plain), "hash": hashlib.sha256(plain).hexdigest()}
        base = len(pool)
        local, glob = dict(rec), dict(rec)
        if explicit:
            local["segments"], glob["segments"] = lsegs, [x + base for x in lsegs]
        else:
            local["seg_range"] = {"start": 0, "stride": 2, "count": len(lsegs)}
            glob["seg_range"] = {"start": base, "stride": 2, "count": len(lsegs)}
        pool.extend({**e, "ref": e["ref"] + base} if "ref" in e else e for e in lpool)
        files.append(glob); shards.append((lpool, local))
    nseg = g
    packed_glob, pack_shard, packed_ids = [], None, set()
    if small:
        ppool, pentries, precs = build_packs(small, aes, articles)
        base = len(pool)
        pool.extend({**e, "ref": e["ref"] + base} if "ref" in e else e for e in ppool)
        packed_glob = [{**pe, "msgid_pool_index": pe["msgid_pool_index"] + base} for pe in pentries]
        files.extend({**r, "segments": []} for r in precs)
        pack_shard = (ppool, [{**r, "segments": []} for r in precs], pentries)
        packed_ids = {r["file_id"] for r in precs}

    def envelope(doc: dict) -> bytes:
        comp = lzma.compress(json.dumps(doc, separators=(",", ":")).encode())
        n = secrets.token_bytes(12); ct_tag = aes.encrypt(n, comp, None)
        return n + ct_tag[-16:] + ct_tag[:-16]

    def post_parts(blob: bytes) -> list:
        half = len(blob) // 2
        parts = []
        for pn, chunk in enumerate([blob[:half], blob[half:]], 1):
            m, c = rand_msgid(), rand_msgid()
            articles[m] = yenc_article(chunk); articles[c] = yenc_article(chunk)
            parts.append({"part_number": pn, "primary_msgid": m, "redundancy_msgids": [c], "size": len(chunk)})
        return parts

    priv = Ed25519PrivateKey.generate()
    pub_raw = priv.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
    pem = priv.public_key().public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo).decode()
    fp = hashlib.sha256(pub_raw).hexdigest()
    manifest = {
        "format": "core_index" if sharded else "share_v4_manifest", "version": 5 if sharded else 4,
        "share_id": share_id, "folder_id": folder_id,
        "owner_public_key": pem, "owner_key_fingerprint": fp, "created_at": "2026-10-25T00:00:00Z",
        "folder_version": 1, "prev_version_message_id": "",
        "access": {"mode": "public", "folder_key_hex": key.hex()},
    }
    index_msgids = []
    if sharded:
        shard_list = []
        docs = [{"msgid_pool": lp, "files": [loc]} for lp, loc in shards]
        if pack_shard:
            docs.append({"msgid_pool": pack_shard[0], "files": pack_shard[1], "packed_segments": pack_shard[2]})
        for si, doc in enumerate(docs):
            blob = envelope({"shard_index": si, "segment_size": SEGMENT_SIZE, **doc})
            parts = post_parts(blob)
            shard_list.append({"shard_index": si, "parts": parts, "digest": hashlib.sha256(blob).hexdigest()})
            index_msgids += [m for p in parts for m in (p["primary_msgid"], *p["redundancy_msgids"])]
        manifest["core_index_format"] = "sharded"
        manifest.update(shards=shard_list, num_shards=len(shard_list), shard_key="file_id_mod_n", folder_digest="",
                        shards_root=shards_root(shard_list))
    else:
        core = {
            "version": "2.0", "format": "optimized", "share_id": share_id,
            "folder": {"folder_id": folder_id, "name": "rig", "total_size": sum(f["size"] for f in files), "file_count": len(files)},
            "folder_version": 1, "prev_version_message_id": "", "segment_size": SEGMENT_SIZE,
            "manifest_features": ["seg_range"] if not explicit else [],
            "msgid_pool": pool,
            "files": files,
            "packed_segments": packed_glob,
        }
        parts = post_parts(envelope(core))
        manifest["core_index"] = {"encryption": "folder_key", "total_parts": 2, "parts": parts}
        index_msgids = [m for p in parts for m in (p["primary_msgid"], *p["redundancy_msgids"])]
    sign_manifest(manifest, priv, pub_raw)
    mm, mc = rand_msgid(), rand_msgid()
    mbytes = json.dumps(manifest).encode()
    articles[mm] = yenc_article(mbytes); articles[mc] = yenc_article(mbytes)
    canonical = f"usenet://{mm}+{mc}/{folder_id}/{fp}#{key.hex()}"
    usn = "usn://" + base64.urlsafe_b64encode(b"\x02" + canonical.encode()).decode().rstrip("=")

    return {"usn": usn, "canonical": canonical, "articles": articles, "held": held, "key": key, "manifest": manifest,
            "share_id": share_id, "nseg": nseg, "sha256": files[0]["hash"],
            "index_msgids": index_msgids, "manifest_msgids": [mm, mc], "folder_id": folder_id}

class FakeNNTP(threading.Thread):
    def __init__(self, articles: dict, user="u", password="p"):
        super().__init__(daemon=True)
        self.articles = articles; self.user, self.password = user, password
        self.srv = socket.socket(); self.srv.bind(("127.0.0.1", 0)); self.srv.listen(128)
        self.port = self.srv.getsockname()[1]
        self.hits = {}; self.lock = threading.Lock(); self.conns = 0

    def release_missing(self, held: dict):
        with self.lock:
            self.articles.update(held)

    def hits_for(self, msgids) -> int:
        with self.lock:
            return sum(self.hits.get(m, 0) for m in msgids)

    def run(self):
        while True:
            c, _ = self.srv.accept()
            threading.Thread(target=self.handle, args=(c,), daemon=True).start()

    def handle(self, c):
        with self.lock: self.conns += 1
        f = c.makefile("rb")
        c.sendall(b"200 fake nntp\r\n")
        authed = False
        try:
            for line in f:
                cmd = line.strip().split(b" ", 1)
                op = cmd[0].upper(); arg = cmd[1].decode() if len(cmd) > 1 else ""
                if op == b"AUTHINFO":
                    kind, val = arg.split(" ", 1)
                    if kind.upper() == "USER": c.sendall(b"381 pass\r\n")
                    elif val == self.password: authed = True; c.sendall(b"281 ok\r\n")
                    else: c.sendall(b"481 bad\r\n")
                elif op == b"QUIT":
                    c.sendall(b"205 bye\r\n"); break
                elif not authed:
                    c.sendall(b"480 auth required\r\n")
                elif op in (b"BODY", b"STAT"):
                    mid = arg.strip("<>")
                    with self.lock:
                        art = self.articles.get(mid); self.hits[mid] = self.hits.get(mid, 0) + 1
                    if art is None:
                        c.sendall(b"430 no such article\r\n")
                    elif op == b"STAT":
                        c.sendall(b"223 0 <" + mid.encode() + b">\r\n")
                    else:
                        c.sendall(b"222 0 <" + mid.encode() + b">\r\n" + art + b".\r\n")
                else:
                    c.sendall(b"500 unknown\r\n")
        except (ConnectionError, OSError):
            pass
        finally:
            c.close()
