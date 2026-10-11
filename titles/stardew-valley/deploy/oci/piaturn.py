#!/usr/bin/env python3
"""Pia-shaped TURN client: method attributes AFTER MESSAGE-INTEGRITY, exactly as measured from
Terraria on 2026-10-11 (ALLOCATE: USERNAME REALM NONCE MI REQUESTED-TRANSPORT; CHANNEL-BIND:
USERNAME REALM NONCE MI CHANNEL-NUMBER XOR-PEER-ADDRESS). Allocates, channel-binds a second
local socket as the peer, and checks ChannelData both ways.
Usage: TURN_SECRET=... piaturn.py <server-ip> [port] [peer-ip-override]
Exit 0 = relay works for Pia, 1 = it does not."""
import base64, hashlib, hmac, os, socket, struct, sys, time

SERVER = (sys.argv[1], int(sys.argv[2]) if len(sys.argv) > 2 else 3478)
PEER_IP = sys.argv[3] if len(sys.argv) > 3 else None
MAGIC = 0x2112A442
secret = os.environ["TURN_SECRET"].encode()
user = f"{int(time.time()) + 3600}:tenants/t-00000000-lp1/users/u-piaturnprobe"
pw = base64.b64encode(hmac.new(secret, user.encode(), hashlib.sha1).digest()).decode()


def attr(t, v):
    return struct.pack("!HH", t, len(v)) + v + b"\0" * ((4 - len(v) % 4) % 4)


def pia_msg(mtype, before, after, key):
    """MI over the header+attributes before it (length field ending at MI), then trailing attrs."""
    tid = os.urandom(12)
    body = b"".join(before)
    if key is not None:
        hdr = struct.pack("!HHI", mtype, len(body) + 24, MAGIC) + tid
        body += attr(0x0008, hmac.new(key, hdr + body, hashlib.sha1).digest())
    body += b"".join(after)
    return struct.pack("!HHI", mtype, len(body), MAGIC) + tid + body


def parse(d):
    mtype, ln, _ = struct.unpack("!HHI", d[:8])
    out, i = {}, 20
    while i + 4 <= 20 + ln:
        t, l = struct.unpack("!HH", d[i:i + 4])
        out.setdefault(t, d[i + 4:i + 4 + l])
        i += 4 + l + ((4 - l % 4) % 4)
    return mtype, out


def xaddr(v):
    return socket.inet_ntoa(struct.pack("!I", struct.unpack("!I", v[4:8])[0] ^ MAGIC)), struct.unpack("!H", v[2:4])[0] ^ (MAGIC >> 16)


def xpeer(ip, port):
    return struct.pack("!BBH", 0, 1, port ^ (MAGIC >> 16)) + struct.pack("!I", struct.unpack("!I", socket.inet_aton(ip))[0] ^ MAGIC)


def err(a):
    e = a.get(0x0009, b"\0\0\0\0")
    return f"{e[2] * 100 + e[3]} {e[4:].decode(errors='replace')}"


s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(3)


def rt(m):
    s.sendto(m, SERVER)
    return parse(s.recv(4096))


transport = attr(0x0019, b"\x11\0\0\0")
mt, a = rt(pia_msg(0x0003, [], [transport], None))
realm, nonce = a[0x0014], a[0x0015]
key = hashlib.md5(user.encode() + b":" + realm + b":" + pw.encode()).digest()
auth = [attr(0x0006, user.encode()), attr(0x0014, realm), attr(0x0015, nonce)]
mt, a = rt(pia_msg(0x0003, auth, [transport], key))
if mt != 0x0103:
    print("ALLOCATE failed:", err(a))
    sys.exit(1)
relay = xaddr(a[0x0016])
print("ALLOCATE ok, relay", relay)

p = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
p.settimeout(3)
if PEER_IP:
    p.bind((PEER_IP, 0))
    peer = p.getsockname()
else:
    p.sendto(struct.pack("!HHI", 1, 0, MAGIC) + os.urandom(12), SERVER)
    peer = xaddr(parse(p.recv(4096))[1][0x0020])
print("peer", peer)

chan = 0x4001
mt, a = rt(pia_msg(0x0009, auth, [attr(0x000C, struct.pack("!HH", chan, 0)), attr(0x0012, xpeer(*peer))], key))
if mt != 0x0109:
    print("CHANNEL-BIND failed:", err(a))
    sys.exit(1)
print("CHANNEL-BIND ok")

# peer first: a home NAT drops the relay's datagram to the peer until the peer has sent to it
p.sendto(b"world", relay)
try:
    d = s.recv(4096)
    ch, ln = struct.unpack("!HH", d[:4])
    print(f"peer->client: ChannelData ch={ch:#x}", d[4:4 + ln])
    ok = ch == chan and d[4:4 + ln] == b"world"
except socket.timeout:
    print("peer->client: NOTHING")
    sys.exit(1)
s.sendto(struct.pack("!HH", chan, 5) + b"hello\0\0\0", SERVER)
try:
    d, src = p.recvfrom(4096)
    print("client->peer via channel:", d, "from", src)
except socket.timeout:
    print("client->peer via channel: NOTHING")
    sys.exit(1)
sys.exit(0 if ok and d == b"hello" else 1)
