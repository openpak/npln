#!/usr/bin/env python3
"""Map IL2CPP v27 method tokens to native addresses in a decompressed Switch NSO.

Inputs must remain outside Git. Output contains names/addresses, never code
or payloads. Optional --calls needs capstone and prints direct call targets.
This supports the 88-byte v27 type layout, not every IL2CPP metadata version.
"""

import argparse
import bisect
import re
import struct
from pathlib import Path


class Image:
    def __init__(self, path):
        self.data = bytearray(Path(path).read_bytes())
        if self.data[:4] != b"NSO0":
            raise ValueError("expected a decompressed NSO")
        if struct.unpack_from("<I", self.data, 12)[0] & 7:
            raise ValueError("decompress the NSO segments first")
        self.segments = [struct.unpack_from("<III", self.data, o)
                         for o in (0x10, 0x20, 0x30)]

    def offset(self, address):
        for file_offset, memory_offset, size in self.segments:
            if memory_offset <= address < memory_offset + size:
                return file_offset + address - memory_offset
        raise ValueError(f"address outside file-backed segments: {address:#x}")

    def address(self, offset):
        for file_offset, memory_offset, size in self.segments:
            if file_offset <= offset < file_offset + size:
                return memory_offset + offset - file_offset
        raise ValueError(f"file offset outside segments: {offset:#x}")

    def relocate(self):
        text, base, _ = self.segments[0]
        mod = self.offset(base + struct.unpack_from("<I", self.data, text + 4)[0])
        if self.data[mod:mod + 4] != b"MOD0":
            raise ValueError("MOD0 header missing")
        dynamic = mod + struct.unpack_from("<i", self.data, mod + 4)[0]
        tags = {}
        while True:
            tag, value = struct.unpack_from("<qQ", self.data, dynamic)
            dynamic += 16
            if not tag:
                break
            tags[tag] = value
        if 7 not in tags or 8 not in tags:
            return
        entry_size = tags.get(9, 24)
        if entry_size < 24:
            raise ValueError("invalid RELA entry size")
        for relative in range(0, tags[8], entry_size):
            target, info, addend = struct.unpack_from(
                "<QQq", self.data, self.offset(tags[7] + relative))
            if info & 0xFFFFFFFF != 1027:  # R_AARCH64_RELATIVE, module base zero
                continue
            try:
                offset = self.offset(target)
            except ValueError:  # BSS is not represented in the file.
                continue
            struct.pack_into("<Q", self.data, offset, addend & 0xFFFFFFFFFFFFFFFF)

    def module(self, assembly):
        name_offset = self.data.find(b"\0" + assembly.encode() + b"\0")
        if name_offset < 0:
            return None
        name_address = self.address(name_offset + 1)
        start, _, size = self.segments[2]
        end = start + size
        needle = struct.pack("<Q", name_address)
        while (found := self.data.find(needle, start, end)) >= 0:
            _, count, pointers = struct.unpack_from("<QQQ", self.data, found)
            try:
                first = self.offset(pointers)
                last = self.offset(pointers + count * 8 - 1)
                if 0 < count < 100000 and last - first == count * 8 - 1:
                    return count, first
            except ValueError:
                pass
            start = found + 8
        return None


def methods(image, metadata):
    data = Path(metadata).read_bytes()
    if struct.unpack_from("<II", data) != (0xFAB11BAF, 27):
        raise ValueError("expected IL2CPP metadata version 27")
    def table(offset):
        return struct.unpack_from("<II", data, offset)
    strings, _ = table(0x18)
    method_table, method_size = table(0x30)
    types, type_size = table(0xA0)
    images, image_size = table(0xA8)
    if type_size % 88 or method_size % 32 or image_size % 40:
        raise ValueError("unsupported metadata table layout")
    def name(index):
        start = strings + index
        return data[start:data.index(b"\0", start)].decode("utf-8", "replace")
    found = []
    for offset in range(images, images + image_size, 40):
        assembly_name, _, first_type, count = struct.unpack_from("<IiiI", data, offset)
        assembly = name(assembly_name)
        module = image.module(assembly)
        if not module:
            continue
        for type_index in range(first_type, first_type + count):
            offset = types + type_index * 88
            type_name, namespace = struct.unpack_from("<II", data, offset)
            first_method = struct.unpack_from("<i", data, offset + 36)[0]
            method_count = struct.unpack_from("<H", data, offset + 64)[0]
            type_name = (name(namespace) + "." if name(namespace) else "") + name(type_name)
            for method_index in range(first_method, first_method + method_count):
                offset = method_table + method_index * 32
                method_name = struct.unpack_from("<I", data, offset)[0]
                row = struct.unpack_from("<I", data, offset + 20)[0] & 0xFFFFFF
                if 0 < row <= module[0]:
                    address = struct.unpack_from("<Q", image.data, module[1] + (row - 1) * 8)[0]
                    text_base, text_size = image.segments[0][1:]
                    if text_base <= address < text_base + text_size and address:
                        found.append((address, assembly, type_name + "::" + name(method_name)))
    return found


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("nso", help="decompressed main NSO")
    parser.add_argument("metadata", help="global-metadata.dat")
    parser.add_argument("--filter", default=".", help="method-name regex")
    parser.add_argument("--assembly", help="exact assembly name")
    parser.add_argument("--calls", action="store_true", help="list direct named calls (requires capstone)")
    args = parser.parse_args()
    pattern = re.compile(args.filter)
    image = Image(args.nso)
    image.relocate()
    mapped = methods(image, args.metadata)
    names = {address: name for address, _, name in mapped}
    starts = sorted(names)
    if args.calls:
        from capstone import Cs, CS_ARCH_ARM64, CS_MODE_LITTLE_ENDIAN
        disassembler = Cs(CS_ARCH_ARM64, CS_MODE_LITTLE_ENDIAN)
    for address, assembly, name in mapped:
        if (args.assembly and assembly != args.assembly) or not pattern.search(name):
            continue
        print(f"{address:#010x} {assembly} {name}")
        if not args.calls:
            continue
        following = bisect.bisect_right(starts, address)
        if following == len(starts):  # No trustworthy upper boundary.
            continue
        start = image.offset(address)
        code = image.data[start:start + starts[following] - address]
        for instruction in disassembler.disasm(bytes(code), address):
            if instruction.mnemonic in ("bl", "b") and instruction.op_str.startswith("#"):
                destination = int(instruction.op_str[1:], 16)
                if destination in names:
                    print(f"  {instruction.address:#010x} {names[destination]}")


if __name__ == "__main__":
    main()
