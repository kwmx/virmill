#!/usr/bin/python3
"""Build only; never execute the EFI application or launch a VM/device tool.

The one executed native test binary tests wire bytes (optionally with ASan/UBSan) and contains
no EFI application code. All image tools receive newly created regular files.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys

SOURCE = Path(__file__).resolve().parent


def run(args, env=None):
    print("+ " + " ".join(map(str, args)), flush=True)
    return subprocess.run(list(map(str, args)), check=True, env=env)


def normalize_fat16_times(path):
    """Normalize mtools directory timestamps; reject unfamiliar filesystem layout."""
    data = bytearray(path.read_bytes())
    sector = struct.unpack_from("<H", data, 11)[0]
    cluster_sectors = data[13]
    reserved = struct.unpack_from("<H", data, 14)[0]
    fats = data[16]
    root_entries = struct.unpack_from("<H", data, 17)[0]
    fat_sectors = struct.unpack_from("<H", data, 22)[0]
    if sector != 512 or not cluster_sectors or not fat_sectors or not root_entries or fats != 2:
        raise ValueError("expected bounded FAT16 filesystem")
    root = (reserved + fats * fat_sectors) * sector
    data_start = root + ((root_entries * 32 + sector - 1) // sector) * sector
    fat = reserved * sector
    visited = set()

    def directory(offset, size):
        if offset < root or offset + size > len(data):
            raise ValueError("directory outside image")
        for entry in range(offset, offset + size, 32):
            if data[entry] == 0:
                break
            if data[entry] == 0xE5 or data[entry + 11] == 0x0F:
                continue
            data[entry + 13] = 0
            for position in (14, 22):
                struct.pack_into("<H", data, entry + position, 0)
            for position in (16, 18, 24):
                struct.pack_into("<H", data, entry + position, 0x21)  # 1980-01-01
            if data[entry + 11] & 0x10 and data[entry] != ord("."):
                cluster = struct.unpack_from("<H", data, entry + 26)[0]
                while 2 <= cluster < 0xFFF8:
                    if cluster in visited or fat + cluster * 2 + 2 > root:
                        raise ValueError("invalid directory cluster chain")
                    visited.add(cluster)
                    directory(data_start + (cluster - 2) * cluster_sectors * sector, cluster_sectors * sector)
                    cluster = struct.unpack_from("<H", data, fat + cluster * 2)[0]
                if cluster < 0xFFF8:
                    raise ValueError("unterminated directory cluster chain")

    directory(root, root_entries * 32)
    path.write_bytes(data)


def verify_pe(path):
    data = path.read_bytes()
    if data[:2] != b"MZ":
        raise ValueError("missing PE DOS header")
    pe = struct.unpack_from("<I", data, 60)[0]
    if data[pe:pe + 4] != b"PE\0\0":
        raise ValueError("missing PE signature")
    machine, sections, timestamp = struct.unpack_from("<HHI", data, pe + 4)
    optional = pe + 24
    magic = struct.unpack_from("<H", data, optional)[0]
    subsystem = struct.unpack_from("<H", data, optional + 68)[0]
    entrypoint = struct.unpack_from("<I", data, optional + 16)[0]
    import_rva, import_size = struct.unpack_from("<II", data, optional + 112 + 8)
    reloc_rva, reloc_size = struct.unpack_from("<II", data, optional + 112 + 5 * 8)
    if (machine, magic, subsystem, timestamp) != (0x8664, 0x20B, 10, 0):
        raise ValueError("expected timestamp-free x86_64 PE32+ EFI application")
    def mapped(rva, size):
        section_table = optional + struct.unpack_from("<H", data, pe + 20)[0]
        for number in range(sections):
            header = section_table + number * 40
            address, raw_size, raw_offset = struct.unpack_from("<III", data, header + 12)
            if address <= rva and rva + size <= address + raw_size:
                offset = raw_offset + rva - address
                if offset + size <= len(data):
                    return data[offset:offset + size]
        raise ValueError("PE data directory is outside a file-backed section")
    if not entrypoint or not sections or not reloc_rva or not reloc_size:
        raise ValueError("missing entry or base relocations")
    # GNU PE ld includes an empty import-table terminator even without imports.
    if import_size and (import_size > 32 or any(mapped(import_rva, import_size))):
        raise ValueError("unexpected dynamic imports")
    relocations = mapped(reloc_rva, reloc_size)
    position, dir64 = 0, 0
    while position < len(relocations):
        _, size = struct.unpack_from("<II", relocations, position)
        if size < 8 or size % 2 or position + size > len(relocations):
            raise ValueError("malformed PE base relocation block")
        for slot in range(position + 8, position + size, 2):
            kind = struct.unpack_from("<H", relocations, slot)[0] >> 12
            if kind not in (0, 10):
                raise ValueError("expected only x86_64 DIR64/ABSOLUTE relocations")
            dir64 += kind == 10
        position += size
    if not dir64:
        raise ValueError("absolute marker pointer has no DIR64 relocation")
    return {"machine": "x86_64", "format": "PE32+", "subsystem": "EFI_APPLICATION", "timestamp": timestamp,
            "entrypointRVA": entrypoint, "baseRelocationBytes": reloc_size, "dir64Relocations": dir64, "imports": 0}


def verify_link_input(path, objdump):
    # GNU PE linking of ELF GOTPCREL calls can retain an indirect instruction but
    # resolve its operand to function text, which is not a function-pointer slot.
    relocations = subprocess.check_output([objdump, "-r", str(path)], text=True)
    if re.search(r"R_X86_64_(?:REX_)?GOT[A-Z0-9_]*", relocations):
        raise ValueError("ELF GOT relocations are unsupported by this EFI recipe; do not use -fno-plt")


def verify_call_targets(path, objdump):
    disassembly = subprocess.check_output([objdump, "-d", str(path)], text=True)
    # Firmware calls use explicit protocol/table pointers in registers. This
    # fixture has no valid RIP-relative indirect function-pointer slot at all.
    suspect = [line.strip() for line in disassembly.splitlines()
               if re.search(r"\b(?:call|jmp)\s+\*[^#]*\(%rip\)", line)]
    if suspect:
        raise ValueError("unsupported RIP-indirect branch in EFI executable: " + suspect[0])
    return {"ripRelativeIndirectBranches": 0, "elfGOTRelocations": 0}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True, help="new output directory outside the source fixture; existing paths are refused")
    parser.add_argument("--sanitize", action="store_true", help="require locally installed ASan/UBSan runtimes for the native byte tests")
    args = parser.parse_args()
    out = args.out.absolute()
    if out.exists() or out.is_symlink():
        parser.error("output directory already exists; choose a new directory")
    if out.resolve().is_relative_to(SOURCE):
        parser.error("generated output must be outside the source fixture tree")
    required = ("gcc", "objcopy", "objdump", "ld", "mkfs.fat", "mmd", "mcopy")
    tools = {name: shutil.which(name) for name in required}
    missing = [name for name, path in tools.items() if path is None]
    if missing:
        parser.error("missing local tools (nothing downloaded): " + ", ".join(missing))
    out.mkdir(mode=0o700, parents=False)
    env = dict(os.environ, LC_ALL="C", TZ="UTC", SOURCE_DATE_EPOCH="0", MTOOLSRC="/dev/null")
    versions = {}
    for name in required:
        option = "--help" if name == "mkfs.fat" else "-V" if name in ("mmd", "mcopy") else "--version"
        text = subprocess.check_output([tools[name], option], stderr=subprocess.STDOUT, env=env, text=True)
        lines = text.splitlines()
        version = next(line for line in lines if line.startswith("mkfs.fat ")) if name == "mkfs.fat" else lines[0]
        versions[name] = {"path": tools[name], "version": version}
    versions["python"] = {"path": sys.executable, "version": sys.version.splitlines()[0]}
    common = ["-std=c11", "-Wall", "-Wextra", "-Werror", "-O2"]
    unit = out / "test-wire"
    sanitizers = ["-fsanitize=address,undefined", "-fno-omit-frame-pointer"] if args.sanitize else ["-fsanitize=undefined", "-fsanitize-undefined-trap-on-error"]
    run([tools["gcc"], *common, *sanitizers, SOURCE / "test_wire.c", SOURCE / "wire.c", "-o", unit], env)
    run([unit], env)
    objects = []
    for source in ("probe.c", "wire.c"):
        stem = Path(source).stem
        elf = out / (stem + ".o")
        run([tools["gcc"], *common, "-ffreestanding", "-fno-builtin", "-fno-stack-protector", "-fpie",
             "-mno-red-zone", "-maccumulate-outgoing-args", "-fno-asynchronous-unwind-tables", "-fno-unwind-tables",
             "-fno-ident", "-c", SOURCE / source, "-o", elf], env)
        run([tools["objcopy"], "--remove-section=.note*", elf], env)
        verify_link_input(elf, tools["objdump"])
        objects.append(elf)
    efi = out / "BOOTX64.EFI"
    run([tools["ld"], "-mi386pep", "--subsystem", "10", "--entry", "efi_main", "--image-base", "0x100000",
         "--file-alignment", "512", "--section-alignment", "4096", "--enable-reloc-section", "--no-insert-timestamp",
         "--disable-auto-import", "--strip-all", "-o", efi, *objects], env)
    # GNU ld's PE backend accepts ELF inputs and creates the PE/COFF executable.
    pe = verify_pe(efi)
    pe.update(verify_call_targets(efi, tools["objdump"]))
    image = out / "probe-fat.img"
    run([tools["mkfs.fat"], "--invariant", "-F", "16", "-n", "VIRMILLTPM", "-i", "564d5052", "-C", image, "32768"], env)
    run([tools["mmd"], "-i", image, "::/EFI", "::/EFI/BOOT"], env)
    run([tools["mcopy"], "-i", image, efi, "::/EFI/BOOT/BOOTX64.EFI"], env)
    normalize_fat16_times(image)
    readback = out / "boot-readback.efi"
    run([tools["mcopy"], "-i", image, "::/EFI/BOOT/BOOTX64.EFI", readback], env)
    if readback.read_bytes() != efi.read_bytes():
        raise ValueError("FAT executable readback differs")
    normalize_fat16_times(image)
    artifact_names = ("BOOTX64.EFI", "probe-fat.img")
    manifest = {"evidenceClass": "local-build-and-synthetic-wire-tests", "guestExecuted": False, "wireTestSanitizers": sanitizers,
                "tools": versions, "pe": pe, "nvIndex": "0x01564d50", "marker": "VIRMILL-COLD-TPM-NV-PROBE-V1-001",
                "sources": {name: hashlib.sha256((SOURCE / name).read_bytes()).hexdigest() for name in
                            ("build.py", "test_build.py", "efi.h", "probe.c", "wire.h", "wire.c", "test_wire.c")},
                "artifacts": {name: {"bytes": (out / name).stat().st_size,
                                      "sha256": hashlib.sha256((out / name).read_bytes()).hexdigest()} for name in artifact_names}}
    (out / "build-manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    for name in artifact_names:
        (out / name).chmod(0o444)
    print(json.dumps(manifest["artifacts"], indent=2))
    print("Built and byte-checked only. No EFI, guest firmware, TPM, or VM execution occurred.")


if __name__ == "__main__":
    main()
