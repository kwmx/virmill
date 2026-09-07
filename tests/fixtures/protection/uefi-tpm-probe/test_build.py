#!/usr/bin/python3
"""Compile-only regression: reject unsafe ELF GOT calls before PE linking.

No object, EFI program, guest, firmware or device code is executed. Generated
objects are retained in a new directory outside this source fixture for review.
"""
import argparse
from pathlib import Path
import shutil
import subprocess
import sys

sys.dont_write_bytecode = True
import build


def rejected(check, path, objdump):
    try:
        check(path, objdump)
    except ValueError as error:
        print("PASS rejected " + path.name + ": " + str(error), flush=True)
    else:
        raise AssertionError("unsafe artifact was accepted: " + str(path))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True, help="new build directory outside the fixture source")
    parser.add_argument("--old-efi", type=Path, help="optional retained unsafe EFI image; require its linked call rejection")
    args = parser.parse_args()
    out = args.out.absolute()
    if out.exists() or out.is_symlink() or out.resolve().is_relative_to(build.SOURCE):
        parser.error("choose a new output directory outside the fixture source")
    gcc, objdump = shutil.which("gcc"), shutil.which("objdump")
    if not gcc or not objdump:
        parser.error("gcc and objdump are required; nothing is downloaded")
    out.mkdir(mode=0o700)
    flags = ["-std=c11", "-Wall", "-Wextra", "-Werror", "-O2", "-ffreestanding", "-fno-builtin",
             "-fno-stack-protector", "-fpie", "-mno-red-zone", "-maccumulate-outgoing-args",
             "-fno-asynchronous-unwind-tables", "-fno-unwind-tables", "-fno-ident"]
    for tool in (gcc, objdump):
        print(subprocess.check_output([tool, "--version"], text=True).splitlines()[0], flush=True)
    unsafe = out / "probe-unsafe-fno-plt.o"
    build.run([gcc, *flags, "-fno-plt", "-c", build.SOURCE / "probe.c", "-o", unsafe])
    rejected(build.verify_link_input, unsafe, objdump)
    safe = out / "probe-reviewed.o"
    build.run([gcc, *flags, "-c", build.SOURCE / "probe.c", "-o", safe])
    build.verify_link_input(safe, objdump)
    print("PASS reviewed compiler flags contain no unsupported ELF GOT relocations", flush=True)
    if args.old_efi:
        rejected(build.verify_call_targets, args.old_efi, objdump)
    print("PASS compile-only call-target regression; no EFI, guest, firmware or TPM execution", flush=True)


if __name__ == "__main__":
    main()
