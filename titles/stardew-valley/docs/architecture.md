# Architecture

The first milestone has one dependency-free Go process with independent TCP and UDP listeners. Both emit the same allowlisted JSON event model through a shared sink. Transport handling is separate from the command-line configuration so protocol decoders can later be introduced without coupling them to sockets or logging.

The observer is intentionally not a server emulator: it reads at most a configured number of bytes, records only their count, and sends no response. Authentication, presence, sessions, discovery, invitations, relay, and NAT traversal remain absent until observations establish a need.

Security defaults are loopback-only, payload-free logs, no remote/client addresses, bounded reads, and short deadlines. This prevents the research scaffold from becoming an accidental credential collector.

