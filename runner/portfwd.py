#!/usr/bin/env python3
"""Transparently proxies published DinD service ports onto 127.0.0.1 inside gVisor sandboxes."""

import asyncio
import http.client
import json
import os
import signal
import socket
import urllib.parse


def parse_docker_tcp(raw: str):
    raw = (raw or "").strip()
    if not raw or raw.startswith("unix://") or raw.startswith("/"):
        return None
    if not raw.startswith(("tcp://", "http://", "https://")):
        return None
    parsed = urllib.parse.urlparse(raw)
    if not parsed.hostname:
        return None
    port = parsed.port or 2375
    return parsed.hostname, port


def list_public_tcp_ports(host: str, port: int):
    conn = http.client.HTTPConnection(host, port, timeout=2.0)
    try:
        conn.request("GET", "/containers/json")
        resp = conn.getresponse()
        body = resp.read()
        if resp.status != 200:
            return set()
        containers = json.loads(body.decode("utf-8", errors="replace"))
    except Exception:
        return set()
    finally:
        try:
            conn.close()
        except Exception:
            pass

    ports = set()
    if not isinstance(containers, list):
        return ports
    for c in containers:
        if not isinstance(c, dict):
            continue
        for p in c.get("Ports") or []:
            if not isinstance(p, dict):
                continue
            ptype = (p.get("Type") or "tcp").lower()
            pub = p.get("PublicPort")
            if ptype == "tcp" and isinstance(pub, int) and pub > 0 and pub != port:
                ports.add(pub)
    return ports


async def pipe(reader: asyncio.StreamReader, writer: asyncio.StreamWriter):
    try:
        while True:
            data = await reader.read(65536)
            if not data:
                break
            writer.write(data)
            await writer.drain()
    except Exception:
        pass
    finally:
        try:
            writer.close()
        except Exception:
            pass


def set_nodelay(writer: asyncio.StreamWriter):
    sock = writer.get_extra_info("socket")
    if sock is not None:
        try:
            sock.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        except Exception:
            pass


async def handle_client(
    client_reader: asyncio.StreamReader,
    client_writer: asyncio.StreamWriter,
    target_host: str,
    target_port: int,
):
    set_nodelay(client_writer)
    try:
        up_reader, up_writer = await asyncio.wait_for(
            asyncio.open_connection(target_host, target_port),
            timeout=5.0,
        )
    except Exception:
        try:
            client_writer.close()
        except Exception:
            pass
        return

    set_nodelay(up_writer)
    await asyncio.gather(
        pipe(client_reader, up_writer),
        pipe(up_reader, client_writer),
        return_exceptions=True,
    )


async def run_proxy(target_host: str, docker_port: int):
    loop = asyncio.get_running_loop()
    stop_event = asyncio.Event()

    for sig in (signal.SIGTERM, signal.SIGINT):
        try:
            loop.add_signal_handler(sig, stop_event.set)
        except NotImplementedError:
            pass

    servers = {}

    async def sync_ports():
        desired = await asyncio.to_thread(list_public_tcp_ports, target_host, docker_port)
        for port in list(servers.keys()):
            if port not in desired:
                srv = servers.pop(port)
                srv.close()
                try:
                    await srv.wait_closed()
                except Exception:
                    pass
        for port in sorted(desired):
            if port in servers:
                continue
            try:
                srv = await asyncio.start_server(
                    lambda r, w, p=port: handle_client(r, w, target_host, p),
                    host="127.0.0.1",
                    port=port,
                    reuse_address=True,
                )
                servers[port] = srv
            except Exception:
                pass

    try:
        while not stop_event.is_set():
            await sync_ports()
            try:
                await asyncio.wait_for(stop_event.wait(), timeout=0.25)
            except asyncio.TimeoutError:
                pass
    finally:
        for srv in servers.values():
            srv.close()
        for srv in servers.values():
            try:
                await srv.wait_closed()
            except Exception:
                pass


def main():
    endpoint = parse_docker_tcp(os.environ.get("DOCKER_HOST", ""))
    if not endpoint:
        return
    asyncio.run(run_proxy(endpoint[0], endpoint[1]))


if __name__ == "__main__":
    main()
