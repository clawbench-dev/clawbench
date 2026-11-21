#!/usr/bin/env python3
"""Detect unconditional AVX instructions in Go cgo C functions.

GOAMD64=v1 only constrains code emitted by the Go compiler. The C code that
cgo compiles with the host C compiler (gcc/clang) is NOT covered by it. The
gcc shipped in Ubuntu 26.04 (the image behind GitHub Actions' ubuntu-latest)
defaults to -march=x86-64-v3, which makes standard-library cgo code (e.g.
runtime/cgo x_cgo_getstackbound, x_cgo_sigaction, x_cgo_thread_start) emit
AVX/BMI instructions. Those execute unconditionally during process startup
and crash with SIGILL on pre-AVX CPUs (golang/go#82835, clawbench #719).
Other platforms' compilers default to v1 today, but pinning the baseline
explicitly everywhere prevents a toolchain update from reintroducing this.

The Go runtime itself also contains AVX code, but those paths are guarded by
CPUID feature detection and are safe. So scanning the whole binary for AVX
bytes would produce false positives. This script inspects ONLY the cgo C
functions (symbols starting with "x_cgo_"), which are compiled from C and
must stay at the x86-64 baseline.

Usage:
    python3 scripts/check-cgo-avx.py <unstripped-go-binary>

The binary must still have its symbol table: do NOT pass a binary built with
-ldflags "-s -w". In CI, build a separate unstripped detection copy with the
same CGO flags, check it here, then ship the stripped binary.

Exit codes:
    0 - no AVX/VEX instructions found in cgo C functions
    1 - AVX/VEX instructions found (release must fail)
    2 - cannot inspect (no cgo symbols / tool error) -> fail closed
"""

import os
import re
import subprocess
import sys


def go_tool(tool: str, *args: str) -> str:
    """Run a `go tool` subcommand, locating `go` via PATH or GOROOT."""
    go = os.environ.get("GO", "go")
    try:
        return subprocess.check_output(
            [go, "tool", tool, *args], text=True, errors="replace"
        )
    except FileNotFoundError:
        # Fall back to the toolchain in GOROOT/bin when `go` is not on PATH.
        goroot = os.environ.get("GOROOT", "")
        if goroot:
            return subprocess.check_output(
                [os.path.join(goroot, "bin", "go"), "tool", tool, *args],
                text=True, errors="replace",
            )
        raise


def cgo_symbols(binary: str):
    """Return sorted [(address, name)] for text symbols named x_cgo_*."""
    out = go_tool("nm", binary)
    syms = []
    for line in out.splitlines():
        # e.g. "  401ea0 T x_cgo_getstackbound"
        m = re.match(r"\s*([0-9a-fA-F]+)\s+[Tt]\s+(x_cgo_\w+)", line)
        if m:
            syms.append((int(m.group(1), 16), m.group(2)))
    return sorted(syms)


# VEX prefix 0xc4/0xc5 => AVX/AVX2/BMI; EVEX 0x62 => AVX-512. The objdump
# machine-code column for such instructions begins with those bytes.
VEX_LINE = re.compile(
    r"^\s*[0-9a-f]+:\s+((?:c4|c5|62)\s+[0-9a-f]{2}\s+[0-9a-f]{2})"
)


def avx_in_range(binary: str, start: int, end: int):
    """Return the first AVX-encoded instruction line in [start, end)."""
    out = subprocess.check_output(
        [
            "objdump", "-d",
            f"--start-address={hex(start)}",
            f"--stop-address={hex(end)}",
            binary,
        ],
        text=True, errors="replace",
    )
    for line in out.splitlines():
        if VEX_LINE.match(line):
            return line.strip()
    return None


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check-cgo-avx.py <unstripped-go-binary>", file=sys.stderr)
        return 2
    binary = sys.argv[1]
    if not os.path.isfile(binary):
        print(f"error: no such file: {binary}", file=sys.stderr)
        return 2

    try:
        syms = cgo_symbols(binary)
    except Exception as exc:  # noqa: BLE001 - fail closed on any tool error
        print(f"error: cannot read symbols from {binary}: {exc}", file=sys.stderr)
        return 2

    if not syms:
        print(
            "error: no x_cgo_* symbols found. Was the binary built with CGO "
            "enabled and without -ldflags '-s -w'? Refusing to pass.",
            file=sys.stderr,
        )
        return 2

    bad = []
    for i, (addr, name) in enumerate(syms):
        # Function extends to the next cgo symbol, or a generous fixed span
        # for the last one. objdump stops at --stop-address anyway.
        end = syms[i + 1][0] if i + 1 < len(syms) else addr + 0x800
        hit = avx_in_range(binary, addr, end)
        if hit:
            bad.append((name, hit))

    if bad:
        print(f"FAIL: {len(bad)} cgo C function(s) contain AVX/VEX instructions:")
        for name, line in bad:
            print(f"  {name}: {line}")
        print(
            "The C compiler emitted instructions unavailable on pre-AVX "
            "x86-64 CPUs. Pin it with CGO_CFLAGS/CGO_CXXFLAGS=-march=x86-64."
        )
        return 1

    print(f"OK: {len(syms)} x_cgo_* functions, no AVX/VEX instructions")
    return 0


if __name__ == "__main__":
    sys.exit(main())
