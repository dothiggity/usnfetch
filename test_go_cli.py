#!/usr/bin/env python3
"""End-to-end tests of the usnfetch command line (bin/usnfetch.exe, or $USNFETCH_BIN) against
the fake NNTP server and share publisher in test_rig.py: list, fetch with faults, resume,
sharded and ragged indexes, packed small files, signatures."""
import hashlib, json, os, shutil, sqlite3, subprocess, sys, tempfile, time
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import test_rig

HERE = os.path.dirname(os.path.abspath(__file__))
GO = os.environ.get("USNFETCH_BIN") or os.path.join(HERE, "bin", "usnfetch.exe" if os.name == "nt" else "usnfetch")

def run(args):
    p = subprocess.run([GO, *args], capture_output=True, text=True, encoding="utf-8", errors="replace")
    return p.returncode, p.stdout, p.stderr

def main():
    assert os.path.isfile(GO), f"build it first: go build -o bin/ ./cmd/usnfetch ({GO})"
    work = tempfile.mkdtemp(prefix="usngo_")
    idx_dir = os.path.join(work, "index")
    cached = lambda: sorted(n for n in os.listdir(idx_dir) if n.endswith(".usnindex"))
    plain = os.urandom(40 * 700000 + 123457)
    share = test_rig.build_share(plain, work)
    srv = test_rig.FakeNNTP(share["articles"]); srv.start()
    net = ["--server", "127.0.0.1", "--port", str(srv.port), "--ssl", "0", "--user", "u", "--pass", "p", "--connections", "12", "--index-dir", idx_dir]

    # 1. list: fetches the core index once (one article per part) and caches it
    rc, out, err = run(["list", "--link", share["usn"], *net])
    assert rc == 0 and "images/test.img" in out and "indexed in" in err and "signature: verified" in err, (rc, out, err)
    assert len(cached()) == 1 and cached()[0].startswith(share["folder_id"] + "-"), cached()
    idx_hits = srv.hits_for(share["index_msgids"])
    assert idx_hits == 2, idx_hits
    print("PASS list (signature verified, index cached)")

    # 2. faults: primary missing / primary corrupt fall through to the copy; one segment gone everywhere
    outp = os.path.join(work, "out.img")
    rc, out, err = run(["fetch", "--link", share["usn"], "--out", outp, "--json", "--missing-retries", "0", *net])
    res = json.loads(out)
    assert rc == 2 and res["missing"] == [test_rig.FAULT_ALL_MISSING] and not res["failed"], (rc, res, err[-2000:])
    assert res["done"] == share["nseg"] - 1 and "cached in" in err and srv.hits_for(share["index_msgids"]) == idx_hits
    got, s, i = open(outp, "rb").read(), test_rig.SEGMENT_SIZE, test_rig.FAULT_ALL_MISSING
    assert got[:i * s] == plain[:i * s] and got[(i + 1) * s:] == plain[(i + 1) * s:]
    assert os.path.exists(outp + ".usnfetch")
    print(f"PASS fetch with faults ({res['avg_MiBps']} MiB/s on loopback), checkpoint kept")

    # 3. resume: only the missing segment is fetched, then SHA-256 verified
    srv.release_missing(share["held"])
    before = dict(srv.hits)
    rc, out, err = run(["fetch", "--link", share["usn"], "--out", outp, "--json", "--verify", *net])
    res = json.loads(out)
    assert rc == 0 and res["complete"] and res["sha256_ok"] and "resume:" in err, (rc, res, err[-1500:])
    new = [k for k, v in srv.hits.items() if v - before.get(k, 0) > 0 and k not in share["manifest_msgids"]]
    assert len(new) == 1 and new[0] in share["held"], new
    assert not os.path.exists(outp + ".usnfetch")
    print("PASS resume + verify (1 article fetched on resume)")

    # 4. sharded index (v5 signed) with explicit segment lists
    data = {"a/one.bin": os.urandom(3 * 700000 + 11), "a/two.bin": os.urandom(2 * 700000), "a/three.txt": b"tiny\n"}
    sh = test_rig.build_share_files(list(data.items()), work, explicit=True, sharded=True, faults=False)
    srv.release_missing(sh["articles"])
    errs = []
    for p, want in data.items():
        o = os.path.join(work, "x_" + os.path.basename(p))
        rc, out, err = run(["fetch", "--link", sh["usn"], "--file", p, "--out", o, "--json", "--verify", *net])
        assert rc == 0 and json.loads(out)["sha256_ok"] and open(o, "rb").read() == want, (p, rc, err[-1500:])
        errs.append(err)
    assert "3 shards" in errs[0] and "indexed in" in errs[0] and all("cached in" in e for e in errs[1:])
    con = sqlite3.connect(os.path.join(idx_dir, [n for n in cached() if n.startswith(sh["folder_id"])][0]))
    assert con.execute("SELECT COUNT(*) FROM file_segs").fetchone()[0] == 7 and con.execute("SELECT COUNT(*) FROM pool").fetchone()[0] == 14
    assert con.execute("SELECT DISTINCT layout FROM files").fetchall() == [(1,)]; con.close()
    print("PASS sharded index with explicit segment lists")

    # 5. ragged segment sizes -> per-segment offset table
    rag = os.urandom(5 * 700000 + 777)
    sh = test_rig.build_share_files([("rag.bin", rag)], work, ragged=True, faults=False)
    srv.release_missing(sh["articles"])
    o = os.path.join(work, "rag.bin")
    rc, out, err = run(["fetch", "--link", sh["usn"], "--out", o, "--json", "--verify", *net])
    assert rc == 0 and json.loads(out)["sha256_ok"] and open(o, "rb").read() == rag, (rc, err[-1500:])
    con = sqlite3.connect(os.path.join(idx_dir, [n for n in cached() if n.startswith(sh["folder_id"])][0]))
    assert con.execute("SELECT layout FROM files").fetchone() == (2,)
    offs = con.execute("SELECT off, size FROM file_offs ORDER BY seg").fetchall(); con.close()
    assert offs[:3] == [(0, 700000), (700000, 699000), (1399000, 698000)] and sum(s for _, s in offs) == len(rag), offs
    print("PASS ragged segments via offset table")

    # 6. a damaged index is rebuilt, not trusted
    p = os.path.join(idx_dir, [n for n in cached() if n.startswith(share["folder_id"])][0])
    with open(p, "r+b") as f:
        f.truncate(os.path.getsize(p) // 2)
    rc, out, err = run(["list", "--link", share["usn"], *net])
    assert rc == 0 and "indexed in" in err and "images/test.img" in out, (rc, err[-800:])
    print("PASS damaged index rebuilt")

    # 7. packed small files, v4 and v5 (gzip/zstd members, whole-gzip pack, missing primary -> copy, empty file)
    small = {f"pk/small{i:02d}.bin": (os.urandom(i * 7919) if i % 3 else (b"text line %d\n" % i) * (i * 50)) for i in range(1, 13)}
    small["pk/empty.txt"] = b""
    for sharded in (False, True):
        sh = test_rig.build_share_files([("pk/big.bin", os.urandom(2 * 700000 + 5)), *small.items()], work, sharded=sharded, faults=False, pack=True)
        srv.release_missing(sh["articles"])
        rc, out, err = run(["list", "--link", sh["usn"], *net])
        listed = {l.split()[-1]: l for l in out.splitlines()}
        assert rc == 0 and all("packed" in listed[p] for p in small) and "packed" not in listed["pk/big.bin"], out
        for name, want in small.items():
            o = os.path.join(work, f"pk_{sharded}_{os.path.basename(name)}")
            rc, out, err = run(["fetch", "--link", sh["usn"], "--file", name, "--out", o, "--json", *net])
            assert rc == 0 and json.loads(out)["packed"] and open(o, "rb").read() == want, (name, rc, err[-800:])
    print(f"PASS packed small files ({len(small)} files, v4 + sharded)")

    # 8. signatures: tampered manifest, another owner's pin, no pin -> refused
    sh = test_rig.build_share_files([("sig/x.bin", os.urandom(800000))], work, faults=False)
    bad = json.loads(json.dumps(sh["manifest"])); bad["created_at"] = "2030-01-01T00:00:00Z"
    tampered = {m: test_rig.yenc_article(json.dumps(bad).encode()) for m in sh["manifest_msgids"]}
    srv_t = test_rig.FakeNNTP({**sh["articles"], **tampered}); srv_t.start()
    net_t = ["--server", "127.0.0.1", "--port", str(srv_t.port), "--ssl", "0", "--user", "u", "--pass", "p", "--index-dir", idx_dir]
    fp = sh["manifest"]["owner_key_fingerprint"]
    for link, why in ((sh["usn"], "signature does not verify"),
                      (sh["canonical"].replace(fp, "ab" * 32), "publisher key mismatch"),
                      ("usenet://" + sh["canonical"].split("//", 1)[1].replace("/" + fp, ""), "no owner fingerprint")):
        rc, out, err = run(["list", "--link", link, *net_t])
        assert rc != 0 and why in err, (why, rc, err[-600:])
    print("PASS signatures: tampering, wrong owner pin, missing pin refused")

    shutil.rmtree(work, ignore_errors=True)        # kept when an assertion fails, for a look
    print("GO CLI PASS")

if __name__ == "__main__":
    main()
