#!/usr/bin/env python3
"""End-to-end test of the Go web app (bin/usnfetch.exe serve) against the fake NNTP rig, over real HTTP:
catalog import -> open share -> tree -> range streaming (segmented + packed) -> availability
check -> download ticked files -> settings never leak passwords -> web password + login."""
import http.cookiejar, io, json, os, shutil, socket, subprocess, sys, tempfile, threading, time, urllib.error, urllib.parse, urllib.request, zipfile
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import test_rig

GO = os.environ.get("USNFETCH_BIN") or os.path.join(os.path.dirname(os.path.abspath(__file__)), "bin", "usnfetch.exe" if os.name == "nt" else "usnfetch")

NNTP_PW = "secretpw1"

class Client:
    def __init__(self, base):
        self.base, self.jar = base, http.cookiejar.CookieJar()
        self.op = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))

    def call(self, method, path, body=None, headers=None, raw=False):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(self.base + path, data=data, method=method, headers={**({"Content-Type": "application/json"} if data else {}), **(headers or {})})
        try:
            with self.op.open(req, timeout=60) as r:
                b = r.read(); return r.status, (b if raw else json.loads(b or b"null")), r.headers
        except urllib.error.HTTPError as e:
            b = e.read()
            try:
                return e.code, json.loads(b), e.headers
            except ValueError:
                return e.code, b, e.headers

def wait_for(fn, timeout=60, what="condition"):
    end = time.time() + timeout
    while time.time() < end:
        v = fn()
        if v:
            return v
        time.sleep(0.2)
    raise AssertionError(f"timeout waiting for {what}")

WORKDIRS = []


def workdir(prefix):
    """A temp folder for one scenario; removed when the whole suite passes (kept on failure)."""
    d = tempfile.mkdtemp(prefix=prefix)
    WORKDIRS.append(d)
    return d


def start_server(work):
    """Start usnfetch serve on a data folder; returns (process, client)."""
    with socket.socket() as so:
        so.bind(("127.0.0.1", 0)); port = so.getsockname()[1]
    log = open(os.path.join(work, "server.log"), "w")
    proc = subprocess.Popen([GO, "serve", "--data", work, "--port", str(port)], stdout=log, stderr=subprocess.STDOUT)
    proc.log = log
    c = Client(f"http://127.0.0.1:{port}")
    wait_for(lambda: c.call("GET", "/api/session")[0] == 200 if proc.poll() is None else (_ for _ in ()).throw(AssertionError(open(os.path.join(work, "server.log")).read())), 20, "go web server")
    return proc, c


def stop(proc):
    proc.terminate()
    try:
        proc.wait(10)
    except subprocess.TimeoutExpired:
        proc.kill()
    proc.log.close()


def setup_and_priorities():
    """First-run setup over the API, the older single-account settings moved into the provider
    list, and provider priorities: an article missing at priority 0 comes from priority 1."""
    # 1. a fresh install: setup needed, folder checks, finish refused without a provider
    work = workdir("usngoweb_setup_")
    proc, c = start_server(work)
    try:
        st = c.call("GET", "/api/status")[1]
        assert st["setup_done"] is False and st["needs_setup"], st
        info = c.call("GET", "/api/setup")[1]
        assert info["setup_done"] is False and info["accounts"] == [] and info["index_dir_default"].endswith("index_cache"), info
        dl = os.path.join(work, "dl", "new")
        r = c.call("POST", "/api/setup/folder", {"path": dl})[1]
        assert r["ok"] and not r["exists"] and r["free"] > 0, r
        assert c.call("POST", "/api/setup/folder", {"path": "relative\\dir"})[1]["ok"] is False
        assert c.call("POST", "/api/setup/folder", {"path": os.path.join(work, "server.log")})[1]["error"] == "that is a file, not a folder"
        assert c.call("POST", "/api/setup/finish", {"complete_dir": dl})[0] == 400        # no provider yet
        code, sv, _ = c.call("PUT", "/api/settings", {"accounts": [{"server": "news.example.invalid", "username": "u", "password": "pw", "priority": 0},
                                                                  {"server": "block.example.invalid", "priority": 3}]})
        assert code == 200 and [a["priority"] for a in sv["accounts"]] == [0, 3], sv["accounts"]
        code, sv, _ = c.call("POST", "/api/setup/finish", {"complete_dir": dl, "index_dir": ""})
        assert code == 200 and sv["setup_done"] and sv["complete_dir"] == dl and os.path.isdir(dl), sv
        assert c.call("GET", "/api/status")[1]["setup_done"] is True
    finally:
        stop(proc)
    saved = json.load(open(os.path.join(work, "usnfetch_gui.json"), encoding="utf-8"))
    assert saved["setup_done"] and "nntp" not in saved and saved["accounts"][1]["priority"] == 3, saved

    # 2. an older settings file: its single account becomes the first provider, setup counts as done
    work = workdir("usngoweb_legacy_")
    with open(os.path.join(work, "usnfetch_gui.json"), "w", encoding="utf-8") as f:
        json.dump({"nntp": {"server": "old.example.invalid", "port": 563, "ssl": True, "username": "olduser", "password": "oldpw", "connections": 12},
                   "accounts": [], "complete_dir": os.path.join(work, "complete")}, f)
    proc, c = start_server(work)
    try:
        sv = c.call("GET", "/api/settings")[1]
        a = sv["accounts"]
        assert len(a) == 1 and a[0]["server"] == "old.example.invalid" and a[0]["username"] == "olduser" and a[0]["connections"] == 12 \
            and a[0]["has_password"] and a[0]["priority"] == 0 and sv["setup_done"] and "nntp" not in sv, sv
    finally:
        stop(proc)

    # 3. priorities in a real download: segment 17 is on neither copy at the priority-0 server
    work = workdir("usngoweb_prio_")
    big = os.urandom(20 * 700000 + 31)
    sh = test_rig.build_share_files([("Prio/disk.img", big)], work, faults=True)
    main_srv = test_rig.FakeNNTP(sh["articles"], password=NNTP_PW); main_srv.start()
    fill_srv = test_rig.FakeNNTP(sh["held"], password=NNTP_PW); fill_srv.start()
    acct = lambda i, srv, prio: {"id": i, "name": i, "server": "127.0.0.1", "port": srv.port, "ssl": False, "username": "u",
                                 "password": NNTP_PW, "connections": 4, "enabled": True, "priority": prio}
    with open(os.path.join(work, "usnfetch_gui.json"), "w", encoding="utf-8") as f:
        json.dump({"complete_dir": os.path.join(work, "complete"), "missing_retries": 0, "extract": False,
                   "accounts": [acct("fill", fill_srv, 2), acct("main", main_srv, 0)]}, f)
    proc, c = start_server(work)
    try:
        r = c.call("POST", "/api/jobs", {"text": sh["usn"], "name": "prio"})[1]
        job = wait_for(lambda: next((j for j in c.call("GET", "/api/jobs")[1]["history"] if j["job_id"] == r["jobs"][0]), None), 60, "priority job")
        assert job["state"] == "completed", job
        assert open(os.path.join(job["out_dir"], "disk.img"), "rb").read() == big
        assert sum(main_srv.hits.values()) > 20 and 0 < sum(fill_srv.hits.values()) <= 2, (sum(main_srv.hits.values()), dict(fill_srv.hits))
        st = c.call("GET", "/api/status")[1]
        assert "if those lack an article: fill (priority 2)" in st["accounts"], st["accounts"]
    finally:
        stop(proc)
    print("PASS first-run setup, older settings migrated, provider priorities (fill-in from priority 2)")


def main():
    work = workdir("usngoweb_")
    big, readme, sub = os.urandom(3 * 700000 + 77), b"hello from a packed file\n" * 40, os.urandom(30000)
    rom = os.urandom(5000)
    zbuf = io.BytesIO()
    with zipfile.ZipFile(zbuf, "w") as z:
        z.writestr("neo.rom", rom)
    cover = b"\x89PNG\r\n\x1a\n" + os.urandom(900)          # what the gallery streams; content is not decoded
    lbxml = ('<?xml version="1.0" standalone="yes"?>\n<LaunchBox>\n  <Game>\n    <Title>Super Mario 64</Title>\n'
             '    <ApplicationPath>Games\\N64\\Mario 64 (USA).n64</ApplicationPath>\n  </Game>\n</LaunchBox>\n').encode()
    share = test_rig.build_share_files([("Pack/disk.img", big), ("Pack/readme.txt", readme), ("Pack/sub/a.bin", sub),
                                        ("Pack/bios/neo.zip", zbuf.getvalue()),
                                        ("Pack/Games/N64/Mario 64 (USA).n64", os.urandom(4000)), ("Pack/Games/N64/Zelda (USA).n64", os.urandom(4000)),
                                        ("Pack/Images/N64/Box - Front/North America/Super Mario 64-01.png", cover),
                                        ("Pack/Images/N64/Box - Front/Japan/Super Mario 64-01.png", os.urandom(500)),
                                        ("Pack/Data/Platforms/N64.xml", lbxml)], work, faults=False, pack=True)
    srv = test_rig.FakeNNTP({**share["articles"], **share["held"]}, password=NNTP_PW); srv.start()

    sp = os.path.join(work, "usnfetch_gui.json")
    s = {}                                   # usnfetch fills in the defaults for the rest
    s.update(complete_dir=os.path.join(work, "complete"), index_dir=os.path.join(work, "index"), missing_retries=0,
             extract=True, extract_standalone=False, delete_archives=True)
    s["accounts"] = [{"id": "a1", "name": "Rig", "server": "127.0.0.1", "port": srv.port, "ssl": False, "username": "u",
                      "password": NNTP_PW, "connections": 6, "enabled": True, "priority": 0}]
    with open(sp, "w", encoding="utf-8") as f:
        json.dump(s, f)
    with socket.socket() as so:
        so.bind(("127.0.0.1", 0)); port = so.getsockname()[1]
    log = open(os.path.join(work, "server.log"), "w")
    proc = subprocess.Popen([GO, "serve", "--data", work, "--port", str(port)], stdout=log, stderr=subprocess.STDOUT)
    c = Client(f"http://127.0.0.1:{port}")
    wait_for(lambda: (lambda: (c.call("GET", "/api/session")[0] == 200))() if proc.poll() is None else (_ for _ in ()).throw(AssertionError(open(os.path.join(work, "server.log")).read())), 20, "go web server")
    try:
        # page + session
        code, page, _ = c.call("GET", "/", raw=True)
        assert code == 200 and b"app.js" in page
        assert c.call("GET", "/static/app.js", raw=True)[0] == 200
        for state in ("idle", "busy", "paused"):                       # the app icons, built in
            code, png, hd = c.call("GET", f"/static/icon-{state}.png", raw=True)
            assert code == 200 and png[:8] == b"\x89PNG\r\n\x1a\n" and hd["Content-Type"] == "image/png", (state, code)
        assert b"/static/icon-idle.png" in page
        assert c.call("GET", "/api/status")[1]["can_restart"] is False          # only the tray copy restarts itself
        assert c.call("POST", "/api/system/restart", {})[0] == 400
        code, sess, _ = c.call("GET", "/api/session")
        assert sess["logged_in"] and not sess["auth_required"], sess

        # catalog import
        text = f"SOURCE: rig  (1 build, 1 link)\n\nThe Rig Pack  [2MB]\nSKU: RIG1\n{share['usn']}\n"
        code, r, _ = c.call("POST", "/api/catalog/import", {"text": text, "name": "rig.txt"})
        assert code == 200 and r["added"] == 1, r
        cat = c.call("GET", "/api/catalog")[1]
        e = cat["entries"][0]
        assert e["title"] == "The Rig Pack" and e["source"] == "rig" and e["share"] is None, e

        # open the share, browse
        code, sh, _ = c.call("POST", "/api/shares/open", {"catalog_id": e["id"]})
        assert code == 200, sh
        sid = sh["id"]
        sh = wait_for(lambda: (lambda x: x if x["state"] != "loading" else None)(c.call("GET", f"/api/shares/{sid}")[1]), 30, "share ready")
        assert sh["state"] == "ready", sh
        assert sh["info"]["files"] == 9 and sh["info"]["packed"] == 8 and sh["info"]["access"] == "public", sh["info"]
        root = c.call("GET", f"/api/shares/{sid}/tree")[1]
        assert [f["name"] for f in root["folders"]] == ["Pack"] and not root["files"], root
        t = c.call("GET", f"/api/shares/{sid}/tree?path=Pack")[1]
        files = {f["name"]: f for f in t["files"]}
        assert [f["name"] for f in t["folders"]] == ["bios", "Data", "Games", "Images", "sub"] and set(files) == {"disk.img", "readme.txt"}, t
        assert files["readme.txt"]["packed"] and not files["disk.img"]["packed"], files

        # range streaming straight from Usenet
        url = f"/api/shares/{sid}/file/{files['disk.img']['fid']}"
        code, body, hd = c.call("GET", url, headers={"Range": "bytes=699990-1400009"}, raw=True)
        assert code == 206 and body == big[699990:1400010] and hd["Content-Range"] == f"bytes 699990-1400009/{len(big)}", (code, len(body), hd)
        code, body, _ = c.call("GET", url, headers={"Range": "bytes=-10"}, raw=True)
        assert code == 206 and body == big[-10:]
        code, body, hd = c.call("GET", url + "?download=1", raw=True)
        assert code == 200 and body == big and "attachment" in hd["Content-Disposition"]
        code, body, _ = c.call("GET", f"/api/shares/{sid}/file/{files['readme.txt']['fid']}", raw=True)
        assert code == 200 and body == readme
        assert c.call("GET", url, headers={"Range": f"bytes={len(big)}-"}, raw=True)[0] == 416
        print("PASS catalog import, share browse, range streaming (segmented + packed)")

        # availability check
        c.call("POST", f"/api/shares/{sid}/check", {"sample": 1000})
        sh = wait_for(lambda: (lambda x: x if not x["checking"] and x["check"] else None)(c.call("GET", f"/api/shares/{sid}")[1]), 30, "check")
        ck = sh["check"]                                         # 4 segments + 1 pack (whose primary is missing: copy counts)
        assert ck["verdict"] == "available" and ck["sampled"] >= 5 and ck["accounts"][0]["primary_ok"] == 4, ck
        assert c.call("GET", "/api/catalog")[1]["entries"][0]["health"]["verdict"] == "available"
        print("PASS availability check (stored on the catalog entry)")

        # download ticked file + folder
        code, r, _ = c.call("POST", f"/api/shares/{sid}/download", {"paths": ["Pack/readme.txt"], "folders": ["Pack/sub"], "category": "games"})
        assert code == 200 and r["files"] == 2, r
        job = wait_for(lambda: next((j for j in c.call("GET", "/api/jobs")[1]["history"] if j["job_id"] == r["job"]), None), 60, "job done")
        assert job["state"] == "completed", job
        out = job["out_dir"]
        assert open(os.path.join(out, "readme.txt"), "rb").read() == readme and open(os.path.join(out, "sub", "a.bin"), "rb").read() == sub
        assert not os.path.exists(os.path.join(out, "disk.img")), os.listdir(out)
        assert c.call("GET", "/api/catalog")[1]["entries"][0]["job"]["state"] == "completed"
        print("PASS download of ticked file + folder via the job queue")

        # search: every word somewhere in the path, at least one in the name itself
        def search(q, path=""):
            return c.call("GET", f"/api/shares/{sid}/search?q={urllib.parse.quote(q)}&path={urllib.parse.quote(path)}")[1]
        r = search("neo")
        assert [f["path"] for f in r["files"]] == ["Pack/bios/neo.zip"] and r["files"][0]["folder"] == "Pack/bios" and r["file_matches"] == 1, r
        r = search("pack READ")
        assert [f["name"] for f in r["files"]] == ["readme.txt"] and not r["folders"], r
        r = search("sub")                                       # the folder, not every file inside it
        assert [d["path"] for d in r["folders"]] == ["Pack/sub"] and r["files"] == [] and r["folder_matches"] == 1, r
        assert search("readme", "Pack/sub")["file_matches"] == 0 and search("a.bin", "Pack/sub")["file_matches"] == 1
        assert search("100%_")["file_matches"] == 0                # LIKE wildcards are literal
        print("PASS search (words across path and name, folder scope)")

        # sorting by size: folder listings (folders and files) and search hits
        def sizes(items):
            return [x["size"] for x in items]
        t = c.call("GET", f"/api/shares/{sid}/tree?path=Pack&sort=size-desc")[1]
        assert [f["name"] for f in t["files"]] == ["disk.img", "readme.txt"], t["files"]
        fs = sizes(t["folders"]); assert fs == sorted(fs, reverse=True) and len(fs) == 5, fs
        t = c.call("GET", f"/api/shares/{sid}/tree?path=Pack&sort=size-asc")[1]
        assert [f["name"] for f in t["files"]] == ["readme.txt", "disk.img"] and sizes(t["folders"]) == sorted(fs), t
        assert [f["name"] for f in c.call("GET", f"/api/shares/{sid}/tree?path=Pack&sort=name")[1]["files"]] == ["disk.img", "readme.txt"]
        r = c.call("GET", f"/api/shares/{sid}/search?q=64&sort=size-desc")[1]
        hs = sizes(r["files"]); assert len(hs) == 5 and hs == sorted(hs, reverse=True), r["files"]
        r = c.call("GET", f"/api/shares/{sid}/search?q=64&sort=size-asc&limit=2")[1]
        assert sizes(r["files"]) == sorted(hs)[:2] and r["file_matches"] == 5, r
        print("PASS sorting by size (listing, search)")

        # review: size, destination (the share's own top folder is dropped, others kept), archives
        code, plan, _ = c.call("POST", f"/api/shares/{sid}/plan", {"folders": ["Pack/bios"], "category": "games"})
        assert code == 200 and plan["files"] == 1 and plan["size"] == len(zbuf.getvalue()), plan
        assert plan["top"] == "Pack" and plan["example"] == "bios" + os.sep + "…" and plan["extract_default"] == "sets", plan
        assert plan["archives"] == {"rar": 0, "split": 0, "single": 1, "empty": 0} and plan["single_examples"] == ["neo.zip"], plan
        assert plan["out_dir"].endswith(os.path.join("games", plan["name"])) and plan["free"] > 0, plan
        code, plan, _ = c.call("POST", f"/api/shares/{sid}/plan", {})
        assert plan["files"] == 9 and plan["example"] == "", plan
        print("PASS download review (size, destination, archive counts)")

        # a single .zip stays zipped by default; "all" unpacks it; folders keep their place
        def run_job(body):
            code, r, _ = c.call("POST", f"/api/shares/{sid}/download", body)
            assert code == 200, r
            return wait_for(lambda: next((j for j in c.call("GET", "/api/jobs")[1]["history"] if j["job_id"] == r["job"]), None), 60, "job done")
        job = run_job({"folders": ["Pack/bios"], "category": "games"})
        bios = os.path.join(job["out_dir"], "bios")
        assert job["state"] == "completed" and "1 single .zip/.7z kept" in job["detail"], job
        assert os.listdir(bios) == ["neo.zip"], os.listdir(bios)
        job = run_job({"folders": ["Pack/bios"], "category": "games", "extract": "all"})
        assert job["state"] == "completed" and job["extract_mode"] == "all" and "1 archive set(s) extracted" in job["detail"], job
        assert os.listdir(bios) == ["neo.rom"] and open(os.path.join(bios, "neo.rom"), "rb").read() == rom, os.listdir(bios)
        assert open(os.path.join(job["out_dir"], "readme.txt"), "rb").read() == readme   # the earlier job's files are untouched
        assert c.call("POST", f"/api/shares/{sid}/download", {"folders": ["Pack/bios"], "extract": "bogus"})[0] == 400
        print("PASS extract modes per download, folder layout kept")

        # what is already on disk: marked while browsing, counted in the review, skipped or verified
        t = c.call("GET", f"/api/shares/{sid}/tree?path=Pack")[1]
        st = {f["name"]: f.get("local") for f in t["files"]}
        assert st == {"readme.txt": "ok", "disk.img": None}, st
        assert {d["name"]: d["local"] for d in t["folders"]} == {"bios": True, "Data": False, "Games": False, "Images": False, "sub": True}, t["folders"]
        r = c.call("POST", f"/api/shares/{sid}/ondisk", {"paths": ["Pack/readme.txt", "Pack/disk.img"], "folders": ["Pack/sub"], "category": "games"})[1]
        assert r == {"counted": True, "files": 2, "bytes": len(readme) + len(sub), "partial": 0, "of": 3, "dir": job["out_dir"]}, r
        assert c.call("POST", f"/api/shares/{sid}/ondisk", {"folders": ["Pack/sub"], "category": "Default"})[1]["files"] == 0
        with open(os.path.join(job["out_dir"], "sub", "a.bin"), "r+b") as f:        # damage one copy, same size
            f.write(b"\0" * 16)
        job = run_job({"paths": ["Pack/readme.txt"], "folders": ["Pack/sub"], "category": "games"})
        assert job["state"] == "completed" and "(2 already on disk)" in job["detail"], job
        assert open(os.path.join(job["out_dir"], "sub", "a.bin"), "rb").read() != sub      # skipped by size: the damage stays
        job = run_job({"paths": ["Pack/readme.txt"], "folders": ["Pack/sub"], "category": "games", "existing": "verify"})
        assert "(1 already on disk)" in job["detail"], job                                    # readme verified, a.bin fetched again
        assert open(os.path.join(job["out_dir"], "sub", "a.bin"), "rb").read() == sub
        assert c.call("POST", f"/api/shares/{sid}/download", {"folders": ["Pack/sub"], "existing": "nope"})[0] == 400
        print("PASS files already on disk: browse marks, review count, skip by size, verify by SHA-256")

        # gallery: LaunchBox layout -> title from the platform XML, North America cover first
        t = c.call("GET", f"/api/shares/{sid}/tree?path=Pack/Games/N64")[1]
        assert t["gallery"] == "launchbox", t
        g = c.call("GET", f"/api/shares/{sid}/gallery?path=Pack/Games/N64")[1]
        assert g["kind"] == "launchbox" and g["platform"] == "N64" and g["matched"] == 1, g
        items = {i["file"]["name"]: i for i in g["items"]}
        mario, zelda = items["Mario 64 (USA).n64"], items["Zelda (USA).n64"]
        assert mario["title"] == "Super Mario 64" and zelda["title"] == "Zelda" and zelda["art"] is None, g
        code, body, hd = c.call("GET", f"/api/shares/{sid}/file/{mario['art']}", raw=True)
        assert code == 200 and body == cover and "max-age" in hd["Cache-Control"], (code, len(body))
        assert c.call("GET", f"/api/shares/{sid}/tree?path=Pack")[1]["gallery"] == ""
        print("PASS gallery (LaunchBox titles from the platform XML, cover by region)")

        # pause, speed limit, schedule, queue order
        th = c.call("POST", "/api/queue/pause", {"minutes": 0})[1]
        assert th["paused"] and th["manual"] and c.call("GET", "/api/status")[1]["throttle"]["paused"], th
        ids = []
        for body in ({"folders": ["Pack/sub"], "category": "tv"}, {"folders": ["Pack/bios"], "category": "tv"}, {"paths": ["Pack/readme.txt"], "category": "tv"}):
            ids.append(c.call("POST", f"/api/shares/{sid}/download", body)[1]["job"])
        time.sleep(1.5)                                          # paused: nothing starts
        q = c.call("GET", "/api/jobs")[1]
        assert [j["job_id"] for j in q["queue"]] == ids and all(j["state"] == "queued" for j in q["queue"]) and q["throttle"]["paused"], q["queue"]
        tr = c.call("GET", "/api/status")[1]["transfer"]                  # what the status bar shows
        assert [t["id"] for t in tr] == ids and all(t["state"] == "queued" and t["size"] >= 0 for t in tr), tr
        assert c.call("POST", "/api/jobs/reorder", {"ids": [ids[2], ids[0], ids[1]]})[0] == 200
        assert [j["job_id"] for j in c.call("GET", "/api/jobs")[1]["queue"]] == [ids[2], ids[0], ids[1]]
        code, sv, _ = c.call("PUT", "/api/settings", {"speed_limit_mbps": 5, "schedule": [{"enabled": True, "days": [], "from": "00:00", "to": "00:00", "action": "limit", "limit_mbps": 2}]})
        assert code == 200 and sv["speed_limit_mbps"] == 5 and sv["schedule"][0]["limit_mbps"] == 2, sv
        assert c.call("PUT", "/api/settings", {"schedule": [{"from": "8", "to": "x", "action": "pause"}]})[0] == 400
        th = c.call("POST", "/api/queue/resume", {})[1]
        assert not th["paused"] and th["limit"] == 2e6 and th["reason"].startswith("schedule"), th

        def all_done():
            h = c.call("GET", "/api/jobs")[1]["history"]
            got = [x for x in h if x["job_id"] in ids]
            return got if len(got) == len(ids) else None
        finished = sorted(wait_for(all_done, 60, "queued jobs done"), key=lambda x: x["finished"])
        assert [x["job_id"] for x in finished] == [ids[2], ids[0], ids[1]] and all(x["state"] == "completed" for x in finished), [(x["job_id"], x["state"]) for x in finished]
        c.call("PUT", "/api/settings", {"speed_limit_mbps": 0, "schedule": []})
        assert c.call("GET", "/api/status")[1]["throttle"]["limit"] == 0
        print("PASS pause / resume, speed limit + schedule, queue reordering")

        # settings never return secrets; an empty password field keeps the stored one
        code, sv, _ = c.call("GET", "/api/settings")
        raw = json.dumps(sv)
        assert NNTP_PW not in raw and sv["accounts"][0]["has_password"] and sv["accounts"][0]["password"] == "", sv["accounts"]
        assert "secret" not in sv["web"] and "password_hash" not in sv["web"]
        acc = dict(sv["accounts"][0], connections=9)
        code, sv, _ = c.call("PUT", "/api/settings", {"accounts": [acc], "max_parallel_jobs": 2})
        assert code == 200 and sv["accounts"][0]["connections"] == 9 and sv["max_parallel_jobs"] == 2, sv
        stored = json.load(open(sp, encoding="utf-8"))["accounts"][0]["password"]   # kept, and sealed at rest on Windows
        assert stored and (stored.startswith("dpapi:") if os.name == "nt" else stored == NNTP_PW), stored
        code, r, _ = c.call("POST", "/api/settings/test-account", sv["accounts"][0])
        assert r["ok"], r
        print("PASS settings masking, edit keeps password, account test")

        # web password: others need to log in
        code, r, _ = c.call("POST", "/api/settings/web-password", {"new": "hunter22pw"})
        assert code == 200 and r["has_password"], r
        assert c.call("GET", "/api/status")[0] == 200                       # the browser that set it got a session
        other = Client(c.base)
        assert other.call("GET", "/api/status")[0] == 401
        assert other.call("GET", "/api/session")[1]["auth_required"]
        assert other.call("POST", "/api/login", {"password": "nope"})[0] == 401
        assert other.call("POST", "/api/login", {"password": "hunter22pw"})[0] == 200
        assert other.call("GET", "/api/status")[0] == 200
        print("PASS web password: login required, wrong password refused, session cookie works")
    finally:
        proc.terminate()
        try:
            proc.wait(10)
        except subprocess.TimeoutExpired:
            proc.kill()
        log.close()
    setup_and_priorities()
    for d in WORKDIRS:
        shutil.rmtree(d, ignore_errors=True)
    print("GO WEB PASS")

if __name__ == "__main__":
    main()
