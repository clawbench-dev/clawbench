#!/usr/bin/env python3
"""Parallel, dependency-light OCI image puller producing a `docker load` tarball.

Why this exists
---------------
In this environment the `docker` CLI talks to the *host* daemon, whose HTTP
proxy resets every external HTTPS CONNECT — so `docker pull` fails for any
registry outside the daemon's no_proxy list. The shell and build containers do
have working egress to registry mirrors, so we fetch manifests + blobs
client-side and stream the result into the daemon via `docker load` (same
principle as `docker cp`: bytes are streamed, no host path resolution).

Registry mirrors throttle *per connection* (~40 KB/s each), so blobs are fetched
as parallel byte-range chunks. Measured: 24 connections ≈ 8 MB/s aggregate.

Run this INSIDE a container (scripts/pull-image.sh does that), because build
containers have fast direct egress.

Usage:
  oci-pull-parallel.py <registry> <repo> <tag> <os/arch> <out.tar> [conns] [local-name]
"""
import concurrent.futures as cf
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time

UA = ("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
      "(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
ACCEPT_INDEX = ", ".join([
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
])
ACCEPT_MANIFEST = ", ".join([
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
])


def curl(args, retries=5):
    last = ""
    for attempt in range(retries):
        p = subprocess.run(["curl", "-sS", "--max-time", "300", *args],
                           capture_output=True)
        if p.returncode == 0:
            return p.stdout
        last = p.stderr.decode(errors="replace").strip()
        time.sleep(1.5 * (attempt + 1))
    raise RuntimeError(f"curl failed: {last}")


def http_get(url, headers, retries=5):
    args = ["-H", f"User-Agent: {UA}"]
    for k, v in headers.items():
        args += ["-H", f"{k}: {v}"]
    args.append(url)
    return curl(args, retries)


def discover_token(registry, repo, tag):
    """Read the WWW-Authenticate challenge off a manifest request and mint a token."""
    out = curl(["-I", "-H", f"User-Agent: {UA}",
                f"{registry}/v2/{repo}/manifests/{tag}"])
    challenge = ""
    for line in out.decode(errors="replace").splitlines():
        if line.lower().startswith("www-authenticate:"):
            challenge = line.split(":", 1)[1].strip()
            break
    if not challenge:
        raise RuntimeError("no WWW-Authenticate challenge (not a v2 registry mirror?)")
    fields = {}
    for part in challenge.replace("Bearer ", "").split(","):
        if "=" in part:
            k, v = part.split("=", 1)
            fields[k.strip()] = v.strip().strip('"')
    scope = fields.get("scope", f"repository:{repo}:pull")
    url = f"{fields['realm']}?service={fields.get('service', '')}&scope={scope}"
    data = json.loads(http_get(url, {"Accept": "application/json"}))
    return data.get("token") or data.get("access_token")


def fetch_blob(registry, repo, digest, size, token_fn, conns, dest_dir, attempts=4):
    """Download one blob (parallel ranges when large), verify digest, return size.

    The WHOLE download is retried when the digest does not match. A registry
    mirror can answer 200 with a truncated or otherwise wrong body; curl exits
    0, so its own `--retry` never fires and the digest is the only thing that
    catches it. Measured in CI (run 36567786806): node:20-alpine's *config* blob
    came back with a different digest on the first try while every other blob of
    that image, and the other image pulled in the same run, were fine — i.e. a
    transient mirror-side corruption. A single bad response must not fail the
    whole image pull, so a mismatch is retried rather than fatal.

    `token_fn` is called before each attempt because tokens are short-lived.
    """
    dest = os.path.join(dest_dir, digest.split(":")[1])
    url = f"{registry}/v2/{repo}/blobs/{digest}"

    def chunk(start, end, out):
        subprocess.run(
            ["curl", "-sSL", "--max-time", "1800", "--retry", "5", "--retry-delay", "2",
             "-o", out, "-r", f"{start}-{end}",
             "-H", f"User-Agent: {UA}", "-H", f"Authorization: Bearer {token}", url],
            check=True)
        # A mirror that ignores the Range header answers 200 with the FULL blob,
        # and a truncated transfer is simply short. Neither is an error at the
        # curl level, so the length is asserted here as well as at the final
        # digest — this localizes the failure to the offending chunk.
        want = end - start + 1
        got = os.path.getsize(out)
        if got != want:
            raise RuntimeError(f"range {start}-{end}: expected {want} bytes, got {got}")

    def clean_partials():
        for p in [dest] + [f"{dest}.part{i}" for i in range(conns)]:
            try:
                os.remove(p)
            except OSError:
                pass

    last = ""
    for attempt in range(attempts):
        token = token_fn()  # tokens are short-lived; re-mint per attempt
        try:
            if size is None or size < 4_000_000 or conns <= 1:
                chunk(0, (size or 1) - 1, dest)
            else:
                n = conns
                step = size // n
                parts = []
                with cf.ThreadPoolExecutor(max_workers=n) as ex:
                    futs = []
                    for i in range(n):
                        start = i * step
                        end = size - 1 if i == n - 1 else start + step - 1
                        part = f"{dest}.part{i}"
                        parts.append(part)
                        futs.append(ex.submit(chunk, start, end, part))
                    for f in futs:
                        f.result()
                with open(dest, "wb") as out:
                    for part in parts:
                        with open(part, "rb") as f:
                            shutil.copyfileobj(f, out, 1 << 20)
                        os.remove(part)
        except Exception as e:  # noqa: BLE001 - any transfer error is retryable
            clean_partials()
            last = f"{type(e).__name__}: {e}"
            if attempt + 1 < attempts:
                print(f"[pull] blob {digest[:19]} attempt {attempt + 1}/{attempts} "
                      f"failed ({last}); retrying", flush=True)
                time.sleep(1.5 * (attempt + 1))
            continue

        got = "sha256:" + hashlib.sha256(open(dest, "rb").read()).hexdigest()
        if got == digest:
            return os.path.getsize(dest)
        last = f"digest mismatch: got {got}"
        os.remove(dest)
        if attempt + 1 < attempts:
            print(f"[pull] blob {digest[:19]} attempt {attempt + 1}/{attempts} "
                  f"{last}; retrying", flush=True)
            time.sleep(1.5 * (attempt + 1))

    raise RuntimeError(f"failed to fetch {digest} after {attempts} attempts: {last}")


def main():
    registry, repo, tag, platform, out_tar = sys.argv[1:6]
    conns = int(sys.argv[6]) if len(sys.argv) > 6 else 24
    local_name = sys.argv[7] if len(sys.argv) > 7 else f"{repo}:{tag}"
    registry = registry.rstrip("/")
    want_os, want_arch = platform.split("/")

    work = tempfile.mkdtemp(prefix="oci-pull-")
    blobs = os.path.join(work, "blobs", "sha256")
    os.makedirs(blobs)
    t0 = time.time()

    print(f"[pull] {registry}/{repo}:{tag} ({platform}), {conns} conns", flush=True)
    token = discover_token(registry, repo, tag)
    idx = json.loads(http_get(f"{registry}/v2/{repo}/manifests/{tag}",
                              {"Accept": ACCEPT_INDEX, "Authorization": f"Bearer {token}"}))
    media = idx.get("mediaType", "")
    if "index" in media or "manifest.list" in media:
        chosen = next((m for m in idx["manifests"]
                       if m.get("platform", {}).get("os") == want_os
                       and m.get("platform", {}).get("architecture") == want_arch), None)
        if chosen is None:
            raise SystemExit(f"no {platform}; have {[m.get('platform') for m in idx['manifests']]}")
        man_raw = http_get(f"{registry}/v2/{repo}/manifests/{chosen['digest']}",
                           {"Accept": ACCEPT_MANIFEST, "Authorization": f"Bearer {token}"})
        man_media = "application/vnd.oci.image.manifest.v1+json"
    else:
        man_raw = json.dumps(idx).encode()
        man_media = media or "application/vnd.oci.image.manifest.v1+json"

    man = json.loads(man_raw)
    cfg, layers = man["config"], man["layers"]
    print(f"[pull] config + {len(layers)} layers, compressed "
          f"{sum(l['size'] for l in layers)/1e6:.0f}MB", flush=True)

    man_digest = "sha256:" + hashlib.sha256(man_raw).hexdigest()
    with open(os.path.join(blobs, man_digest.split(":")[1]), "wb") as f:
        f.write(man_raw)

    total = 0
    for label, blob in [("config", cfg)] + [(f"layer {i}/{len(layers)}", l)
                                            for i, l in enumerate(layers, 1)]:
        n = fetch_blob(registry, repo, blob["digest"], blob.get("size"),
                       lambda: discover_token(registry, repo, tag), conns, blobs)
        total += n
        print(f"[pull] {label:14s} {blob['digest'][:19]} {n/1e6:8.1f}MB "
              f"| {total/1e6:7.1f}MB, {time.time()-t0:5.0f}s", flush=True)

    with open(os.path.join(work, "oci-layout"), "w") as f:
        json.dump({"imageLayoutVersion": "1.0.0"}, f)
    with open(os.path.join(work, "index.json"), "w") as f:
        json.dump({"schemaVersion": 2,
                   "mediaType": "application/vnd.oci.image.index.v1+json",
                   "manifests": [{
                       "mediaType": man_media, "digest": man_digest, "size": len(man_raw),
                       "platform": {"architecture": want_arch, "os": want_os},
                       "annotations": {"org.opencontainers.image.ref.name": local_name,
                                       "io.containerd.image.name": local_name},
                   }]}, f)
    with open(os.path.join(work, "manifest.json"), "w") as f:
        json.dump([{
            "Config": f"blobs/sha256/{cfg['digest'].split(':')[1]}",
            "RepoTags": [local_name],
            "Layers": [f"blobs/sha256/{l['digest'].split(':')[1]}" for l in layers],
        }], f)

    print(f"[pull] packing {out_tar}", flush=True)
    with tarfile.open(out_tar, "w") as tar:
        for root, _dirs, files in os.walk(work):
            for name in files:
                full = os.path.join(root, name)
                tar.add(full, arcname=os.path.relpath(full, work))
    shutil.rmtree(work)
    print(f"[pull] DONE {out_tar} ({os.path.getsize(out_tar)/1e6:.0f}MB) "
          f"in {time.time()-t0:.0f}s", flush=True)


if __name__ == "__main__":
    main()
