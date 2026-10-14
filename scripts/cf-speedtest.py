#!/usr/bin/env python3
"""独立 CF / XHTTP 优选：域名回源筛选 + 临时 mihomo 端到端测速。"""
from __future__ import annotations

import argparse
import concurrent.futures
import copy
import csv
import getpass
import gzip
import hashlib
import ipaddress
import json
import math
import os
import platform
import random
import re
import shutil
import signal
import socket
import statistics
import subprocess
import sys
import tempfile
import time
import urllib.parse
import urllib.request
from pathlib import Path

STATE = Path(os.environ.get("CF_SPEEDTEST_HOME", str(Path.home() / ".config/cf-speedtest"))).expanduser()
VERSION = "v1.19.32"
LATENCY_URL = "https://www.gstatic.com/generate_204"
# This endpoint currently rejects a 16 MiB request with HTTP 403 while 8 MiB
# requests succeed. Split a larger measurement; preserve its total byte budget.
DOWNLOAD_CHUNK_BYTES = 8 * 1048576
# Do not inherit a proxy that could turn a direct edge test into a proxy test.
ENV = {k: v for k, v in os.environ.items() if k.lower() not in
       {"http_proxy", "https_proxy", "all_proxy", "no_proxy"}}
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))
HTTP.addheaders = [("User-Agent", "FireX-CF-SpeedTest/1.0")]


def validate_node(node):
    if not isinstance(node, dict):
        raise ValueError("需要单个 mihomo 节点对象")
    if (node.get("type") != "vless" or node.get("network") != "xhttp" or
            not node.get("tls") or node.get("reality-opts") or node.get("skip-cert-verify")):
        raise ValueError("需要启用证书校验的 VLESS + XHTTP + TLS 节点")
    if not all(node.get(k) for k in ("uuid", "server", "servername", "port", "xhttp-opts")):
        raise ValueError("节点参数缺少 UUID / server / SNI / port / xhttp-opts")
    if not isinstance(node["xhttp-opts"], dict) or not isinstance(node["port"], int) or not 1 <= node["port"] <= 65535:
        raise ValueError("节点端口或 XHTTP 参数无效")
    if node.get("dialer-proxy"):
        raise ValueError("所选节点依赖其他代理，不能独立测速")
    return copy.deepcopy(node)


def read_config(path):
    if not path.exists():
        return {}
    try:
        value = json.loads(path.read_text())
    except (OSError, ValueError):
        raise ValueError("配置文件无法读取或 JSON 格式错误") from None
    if not isinstance(value, dict) or value.get("version") != 1 or not isinstance(value.get("settings", {}), dict):
        raise ValueError("配置文件结构或版本不支持")
    return value


def write_config(path, value):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd, temporary = tempfile.mkstemp(prefix=".cf-config-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(value, f, ensure_ascii=False, indent=2)
            f.write("\n")
        os.chmod(temporary, 0o600)
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


def subscription_nodes(url):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme not in ("http", "https") or not parsed.hostname:
        raise ValueError("订阅链接需要以 http:// 或 https:// 开头")
    try:
        import yaml
    except ImportError:
        raise ValueError("需要 PyYAML；请运行 cf-speedtest.command 自动安装独立运行环境") from None
    req = urllib.request.Request(url, headers={"User-Agent": f"mihomo/{VERSION} FireX-CF-SpeedTest"})
    try:
        with HTTP.open(req, timeout=30) as r:
            raw = r.read(8 * 1024 * 1024 + 1)
    except OSError:
        raise ValueError("订阅获取失败；请检查该客户端能否访问配置的订阅地址") from None
    if len(raw) > 8 * 1024 * 1024:
        raise ValueError("订阅超过 8 MiB，停止解析")
    try:
        document = yaml.safe_load(raw)
    except yaml.YAMLError:
        raise ValueError("订阅解析失败，请使用 Clash / mihomo YAML 或 JSON 格式") from None
    nodes = document.get("proxies") if isinstance(document, dict) else None
    if not isinstance(nodes, list):
        raise ValueError("订阅没有 proxies 节点列表，请使用 Clash / mihomo 格式订阅")
    available = []
    for node in nodes:
        try:
            normalized = validate_node(node)
            if not isinstance(normalized.get("name"), str) or not normalized["name"]:
                continue
            available.append(normalized)
        except ValueError:
            pass
    names = [n["name"] for n in available]
    if len(names) != len(set(names)):
        raise ValueError("订阅中可测速节点名称重复，请先在订阅中设置唯一名称")
    if not available:
        raise ValueError("订阅中没有可独立测速的 VLESS + XHTTP + TLS 节点")
    return available


def choose_node(nodes, name=None):
    if name:
        found = next((n for n in nodes if n["name"] == name), None)
        if found is None:
            raise ValueError("所选节点已不在订阅中，请使用 --bind 重新选择")
        return found
    print("可测速的 CF XHTTP 节点：")
    for index, node in enumerate(nodes, 1):
        print(f"  {index}. {node['name']} ({node['servername']}:{node['port']})")
    if len(nodes) == 1:
        print("只有一个可测速节点，已选择。")
        return nodes[0]
    if not sys.stdin.isatty():
        raise ValueError("需要选择节点，请在终端运行 --bind，或用 --node 指定完整名称")
    while True:
        answer = input("选择节点编号：").strip()
        if answer.isdigit() and 1 <= int(answer) <= len(nodes):
            return nodes[int(answer) - 1]
        print("请输入列表中的编号。")


def load_node(args, config):
    if args.node_json:
        return validate_node(json.loads(args.node_json.read_text()))
    rebinding = args.bind or not config.get("subscription_url")
    url = config.get("subscription_url", "")
    if rebinding:
        if not sys.stdin.isatty():
            raise ValueError("尚未绑定订阅，请先在终端运行 cf-speedtest.command --bind")
        url = getpass.getpass("订阅链接（输入不回显，回车保留已绑定链接）：").strip() or url
        if not url:
            raise ValueError("订阅链接不能为空")
    nodes = subscription_nodes(url)
    if args.list_nodes:
        for index, node in enumerate(nodes, 1):
            print(f"{index}. {node['name']} ({node['servername']}:{node['port']})")
        return None
    selected = choose_node(nodes, args.node or (None if rebinding else config.get("node_name")))
    if rebinding or args.node:
        if rebinding and sys.stdin.isatty() and args.label == "当前网络":
            args.label = input("网络环境名称（例如 上海移动，回车使用“当前网络”）：").strip() or args.label
        value = {"version": 1, "subscription_url": url, "node_name": selected["name"],
                 "settings": {k: getattr(args, k) for k in ("label", "iface", "threads", "shortlist", "top",
                              "reps", "download_mib", "download_timeout", "origin_code",
                              "max_latency_ms", "min_download_mib_s")}}
        write_config(args.config, value)
        print(f"已绑定：{selected['name']}；环境：{args.label}\n配置：{args.config}")
    return selected


def physical_interface(requested):
    if requested:
        socket.if_nametoindex(requested)
        if requested.startswith(("utun", "tun", "tap", "wg")):
            raise ValueError("请指定物理网卡，例如 --iface en0")
        return requested
    if platform.system() == "Darwin":
        r = subprocess.run(["route", "-n", "get", "104.17.26.239"], capture_output=True, text=True)
        found = re.search(r"interface:\s*(\S+)", r.stdout)
        if not found:
            raise ValueError("无法确定出站网卡，请指定 --iface en0")
        return physical_interface(found.group(1))
    if platform.system() == "Linux":
        r = subprocess.run(["ip", "-json", "route", "get", "104.17.26.239"], capture_output=True, text=True)
        return physical_interface(json.loads(r.stdout)[0]["dev"])
    raise ValueError("目前支持 macOS / Linux")


def request_json(url, method="GET", data=None, secret=None, timeout=10):
    headers = {"Content-Type": "application/json", "User-Agent": "FireX-CF-SpeedTest"}
    if secret:
        headers["Authorization"] = "Bearer " + secret
    req = urllib.request.Request(url, data=json.dumps(data).encode() if data is not None else None,
                                 headers=headers, method=method)
    with HTTP.open(req, timeout=timeout) as r:
        body = r.read()
        return json.loads(body) if body else None


def find_mihomo(args):
    known = [args.mihomo] if args.mihomo else [
        shutil.which("mihomo"), STATE / "bin/mihomo", Path(__file__).resolve().parent / "mihomo"]
    for path in known:
        if path and Path(path).is_file() and os.access(path, os.X_OK):
            return str(Path(path).resolve())
    if args.mihomo:
        raise ValueError("--mihomo 指定的内核不存在或不可执行")
    system = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "amd64": "amd64"}.get(platform.machine())
    if not system or not arch:
        raise ValueError("未找到 mihomo，且当前平台不支持自动下载；请使用 --mihomo")
    asset_arch = "amd64-v1" if system == "linux" and arch == "amd64" else arch
    asset = f"mihomo-{system}-{asset_arch}-{VERSION}.gz"
    print(f"未找到 mihomo，下载官方 {VERSION} 并校验 SHA-256…", flush=True)
    release = request_json(f"https://api.github.com/repos/MetaCubeX/mihomo/releases/tags/{VERSION}", timeout=30)
    found = next((a for a in release["assets"] if a["name"] == asset), None)
    if not found or not re.fullmatch(r"sha256:[0-9a-f]{64}", found.get("digest") or ""):
        raise ValueError("官方发布未提供此资源的 SHA-256，停止下载")
    directory = STATE / "bin"
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix=".download-", dir=directory) as temp:
        packed = Path(temp) / "mihomo.gz"
        digest = hashlib.sha256()
        with HTTP.open(found["browser_download_url"], timeout=60) as r, packed.open("wb") as out:
            while block := r.read(1024 * 1024):
                digest.update(block)
                out.write(block)
        if digest.hexdigest() != found["digest"][7:]:
            raise ValueError("mihomo 下载校验失败")
        binary = Path(temp) / "mihomo"
        with gzip.open(packed, "rb") as src, binary.open("wb") as out:
            shutil.copyfileobj(src, out)
        binary.chmod(0o700)
        os.replace(binary, directory / "mihomo")
    return str(directory / "mihomo")


def curl_metrics(extra, timeout, body=False):
    fmt = '\n{"status":%{http_code},"seconds":%{time_total},"bytes":%{size_download},"bytes_per_second":%{speed_download}}'
    cmd = ["curl", "-q", "--silent", "--show-error", "--connect-timeout", "5",
           "--max-time", str(timeout), "--write-out", fmt]
    if not body:
        cmd += ["--output", os.devnull]
    r = subprocess.run(cmd + extra, capture_output=True, text=True, env=ENV, timeout=timeout + 5)
    content, _, metrics = r.stdout.rpartition("\n")
    try:
        result = json.loads(metrics)
    except (ValueError, TypeError):
        result = {"status": 0, "seconds": 0, "bytes": 0, "bytes_per_second": 0}
    result["exit"] = r.returncode
    # Body is kept in memory only; never export node credentials or an origin response.
    return result, content


def download_via_proxy(proxy, size, timeout):
    started = time.monotonic()
    downloaded = 0
    requests = []
    result = {"download_ok": False, "download_mib_s": None, "download_status": 0,
              "download_bytes": 0, "download_expected_bytes": size, "download_requests": requests}
    while downloaded < size:
        remaining = timeout - (time.monotonic() - started)
        if remaining <= 0:
            result["error"] = f"下载超时：累计 {downloaded}/{size} 字节，限时 {timeout} 秒"
            break
        chunk = min(DOWNLOAD_CHUNK_BYTES, size - downloaded)
        transfer, _ = curl_metrics(["--noproxy", "", "--proxy", proxy,
                                    f"https://speed.cloudflare.com/__down?bytes={chunk}"], remaining)
        requests.append({**transfer, "expected_bytes": chunk})
        downloaded += transfer["bytes"]
        result.update({"download_status": transfer["status"], "download_bytes": downloaded,
                       "download_curl_exit": transfer["exit"]})
        if transfer["status"] not in (0, 200):
            result["error"] = (f"测速站返回 HTTP {transfer['status']}："
                               f"本请求收到 {transfer['bytes']}/{chunk} 字节")
            break
        if transfer["exit"] == 28:
            result["error"] = f"下载超时：累计 {downloaded}/{size} 字节，限时 {timeout} 秒"
            break
        if transfer["exit"] != 0:
            result["error"] = (f"下载连接失败：curl {transfer['exit']}，HTTP {transfer['status']}，"
                               f"累计 {downloaded}/{size} 字节")
            break
        if transfer["status"] != 200 or transfer["bytes"] != chunk:
            result["error"] = (f"下载响应不完整：HTTP {transfer['status']}，"
                               f"本请求收到 {transfer['bytes']}/{chunk} 字节")
            break
    elapsed = time.monotonic() - started
    result["download_seconds"] = round(elapsed, 6)
    if not result.get("error") and downloaded == size:
        result["download_ok"] = True
        result["download_mib_s"] = round(downloaded / max(elapsed, 0.001) / 1048576, 3)
    return result


def ipv4(value):
    address = ipaddress.ip_address(value)
    if address.version != 4 or not address.is_global:
        raise ValueError("候选必须是公网 IPv4 地址")
    # Fake IPs must never be mistaken for an actual CDN endpoint.
    return str(address)


def sample_ip(net):
    offset = random.randrange(1, net.num_addresses - 1) if net.num_addresses > 2 else 0
    return str(net.network_address + offset)


def candidates(args, node, iface):
    supplied = bool(args.ips or args.csv or args.ip_file)
    values = list(args.ips or [])
    if args.csv:
        with args.csv.open(encoding="utf-8-sig", newline="") as f:
            for row in csv.reader(f):
                if row:
                    try:
                        values.append(ipv4(row[0].strip()))
                    except ValueError:
                        pass
    if args.ip_file:
        for line in args.ip_file.read_text().splitlines():
            text = line.split("#", 1)[0].strip()
            if text:
                net = ipaddress.ip_network(text, strict=False)
                if net.version == 4:
                    values.append(sample_ip(net))
    if not supplied:
        print("获取 CF 官方 IPv4 网段，每个 /24 随机抽取一个 IP…", flush=True)
        response = subprocess.run(["curl", "-q", "--fail", "--silent", "--show-error", "--noproxy", "*",
                                   "--interface", iface, "--connect-timeout", "5", "--max-time", "20",
                                   "https://www.cloudflare.com/ips-v4"],
                                  capture_output=True, text=True, env=ENV, timeout=25)
        if response.returncode:
            raise ValueError("无法获取 CF 官方网段，可用 --csv / --ips / --ip-file 提供候选")
        ranges = response.stdout.splitlines()
        for line in ranges:
            net = ipaddress.ip_network(line.strip())
            for subnet in net.subnets(new_prefix=24) if net.prefixlen <= 24 else [net]:
                values.append(sample_ip(subnet))
    # Always compare the configured endpoint and the origin-domain DNS baseline.
    controls = []
    for host in dict.fromkeys([node["server"], node["servername"]]):
        try:
            addresses = [ipv4(host)]
        except ValueError:
            addresses = [a[4][0] for a in socket.getaddrinfo(host, node["port"], socket.AF_INET, socket.SOCK_STREAM)]
        for address in addresses:
            address = ipv4(address)
            if address not in controls:
                controls.append(address)
    values += controls
    return list(dict.fromkeys(ipv4(v) for v in values)), controls


def tcp_probe(ip, port, iface):
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        sock.settimeout(1.2)
        if platform.system() == "Darwin":
            sock.setsockopt(socket.IPPROTO_IP, 25, socket.if_nametoindex(iface))
        else:
            sock.setsockopt(socket.SOL_SOCKET, socket.SO_BINDTODEVICE, iface.encode() + b"\0")
        start = time.monotonic()
        sock.connect((ip, port))
        return round((time.monotonic() - start) * 1000, 2)
    except PermissionError:
        raise ValueError("无法绑定物理网卡；请检查当前用户的网卡绑定权限") from None
    except OSError:
        return None
    finally:
        sock.close()


def domain_probe(ip, node, iface, expected):
    sni, port = node["servername"], node["port"]
    host = node["xhttp-opts"].get("host") or sni
    result, body = curl_metrics(["--noproxy", "*", "--interface", iface,
                                "--resolve", f"{sni}:{port}:{ip}", "--header", f"Host: {host}",
                                f"https://{sni}:{port}/"], 7, body=True)
    code = re.search(r"error code:\s*(1\d{3})", body, re.I)
    result.update({"ip": ip, "edge_error": code.group(1) if code else "",
                   "pass": result["exit"] == 0 and result["status"] == expected and not code})
    return result


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


class Core:
    def __init__(self, binary, node, ips, iface):
        self.temp = None
        self.proc = None
        self.log = None
        self.binary, self.node, self.ips, self.iface = binary, node, ips, iface
        self.mixed, self.controller = free_port(), free_port()
        while self.controller == self.mixed:
            self.controller = free_port()
        self.secret = os.urandom(24).hex()
        self.base = f"http://127.0.0.1:{self.controller}"

    def __enter__(self):
        try:
            return self.start()
        except BaseException:
            self.__exit__(None, None, None)
            raise

    def start(self):
        self.temp = tempfile.TemporaryDirectory(prefix="firex-cf-speedtest-")
        work = Path(self.temp.name)
        work.chmod(0o700)
        proxies = []
        for i, address in enumerate(self.ips):
            proxy = copy.deepcopy(self.node)
            proxy.update({"name": f"CF{i}", "server": address})
            proxy["interface-name"] = self.iface
            proxies.append(proxy)
        config = {"mixed-port": self.mixed, "bind-address": "127.0.0.1", "allow-lan": False,
                  "external-controller": f"127.0.0.1:{self.controller}", "secret": self.secret,
                  "mode": "rule", "log-level": "silent", "ipv6": False, "unified-delay": True,
                  "interface-name": self.iface, "tun": {"enable": False}, "dns": {"enable": False},
                  "proxies": proxies, "proxy-groups": [{"name": "TEST", "type": "select",
                  "proxies": [p["name"] for p in proxies]}], "rules": ["MATCH,TEST"]}
        path = work / "config.json"
        path.write_text(json.dumps(config))
        path.chmod(0o600)
        self.log = (work / "mihomo.log").open("w")
        self.proc = subprocess.Popen([self.binary, "-d", str(work), "-f", str(path)],
                                     stdout=self.log, stderr=subprocess.STDOUT, env=ENV)
        deadline = time.monotonic() + 12
        while time.monotonic() < deadline:
            if self.proc.poll() is not None:
                raise ValueError("临时 mihomo 启动失败；请检查内核版本和节点参数")
            try:
                request_json(self.base + "/version", secret=self.secret, timeout=0.5)
                return self
            except (OSError, ValueError):
                time.sleep(0.1)
        raise ValueError("临时 mihomo 启动超时")

    def __exit__(self, *exc):
        if self.proc is not None and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait()
        if self.log is not None:
            self.log.close()
        if self.temp is not None:
            self.temp.cleanup()

    def measure(self, index, size, timeout):
        name = f"CF{index}"
        request_json(self.base + "/proxies/TEST", method="PUT", data={"name": name}, secret=self.secret)
        result = {"latency_ms": None, "download_ok": False, "download_mib_s": None}
        query = urllib.parse.urlencode({"url": LATENCY_URL, "timeout": 10000})
        try:
            result["latency_ms"] = request_json(self.base + f"/proxies/{name}/delay?" + query,
                                                secret=self.secret, timeout=12)["delay"]
        except (OSError, KeyError, ValueError):
            result["error"] = "XHTTP 延迟请求失败"
            return result
        result.update(download_via_proxy(f"http://127.0.0.1:{self.mixed}", size, timeout))
        return result


def summarize(ip, runs, control):
    downloads = [r["download_mib_s"] for r in runs if r["download_ok"]]
    latencies = [r["latency_ms"] for r in runs if r["latency_ms"] is not None]
    return {"ip": ip, "control": control, "success": len(downloads), "rounds": len(runs),
            "median_latency_ms": round(statistics.median(latencies), 1) if latencies else None,
            "median_download_mib_s": round(statistics.median(downloads), 3) if downloads else None}


def qualify(summary, max_latency_ms, min_download_mib_s):
    reasons = []
    if not summary["rounds"] or summary["success"] != summary["rounds"]:
        reasons.append("下载未全部成功")
    latency, speed = summary["median_latency_ms"], summary["median_download_mib_s"]
    if latency is None:
        reasons.append("无有效延迟")
    elif latency > max_latency_ms:
        reasons.append(f"延迟 {latency}>{max_latency_ms:g} ms")
    if speed is None:
        reasons.append("无有效下载速度")
    elif speed < min_download_mib_s:
        reasons.append(f"速度 {speed}<{min_download_mib_s:g} MiB/s")
    return {**summary, "eligible": not reasons, "rejection_reason": "；".join(reasons)}


def choose_recommendation(rows):
    reliable = [r for r in rows if r["rounds"] > 0 and r["success"] == r["rounds"]
                and r["median_latency_ms"] is not None
                and r["median_download_mib_s"] is not None and r["median_download_mib_s"] > 0]
    qualified = [r for r in reliable if r["eligible"]]
    pool = qualified or reliable
    if not pool:
        return None
    best = max(pool, key=lambda r: (r["median_download_mib_s"], -r["median_latency_ms"]))
    return {**best, "fallback": not bool(qualified)}


def main():
    early = argparse.ArgumentParser(add_help=False)
    early.add_argument("--config", type=Path, default=STATE / "config.json")
    early_args, _ = early.parse_known_args()
    config = read_config(early_args.config)
    settings = config.get("settings", {})
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.ArgumentDefaultsHelpFormatter)
    parser.add_argument("--config", type=Path, default=early_args.config, help="本客户端的订阅和测速配置")
    parser.add_argument("--bind", action="store_true", help="绑定或更换订阅、选择节点并保存配置（不测速）")
    parser.add_argument("--list-nodes", action="store_true", help="列出绑定订阅中的可测速节点（不测速）")
    parser.add_argument("--node", help="指定订阅中的完整节点名称，保存选择后测速")
    parser.add_argument("--label", default="当前网络", help="本客户端的网络环境名称，随结果保存")
    parser.add_argument("--node-json", type=Path, help="临时改用单个 mihomo 节点 JSON，不更新订阅绑定")
    parser.add_argument("--mihomo", type=Path, help="指定 mihomo；缺省时查找或下载官方内核")
    parser.add_argument("--iface", help="物理网卡；缺省时自动检测，拒绝使用 TUN 网卡")
    parser.add_argument("--ips", nargs="+", help="仅测这些 IPv4，另加当前入口和 DNS 对照")
    parser.add_argument("--csv", type=Path, help="导入 cfst result.csv 的第一列 IP")
    parser.add_argument("--ip-file", type=Path, help="每行 IPv4 或 CIDR；CIDR 抽一个 IP")
    parser.add_argument("--threads", type=int, default=128, help="TCP 扫描并发")
    parser.add_argument("--shortlist", type=int, default=200, help="进入域名筛选的 TCP 候选数量")
    parser.add_argument("--top", type=int, default=5, help="进入实际 XHTTP 测速的候选数量，另加对照")
    parser.add_argument("--reps", type=int, default=3, help="端到端测速轮数，随机测试顺序")
    parser.add_argument("--download-mib", type=int, default=16, help="每轮每 IP 的完整下载大小 MiB")
    parser.add_argument("--download-timeout", type=int, default=25, help="下载超时秒数")
    parser.add_argument("--max-latency-ms", type=float, default=190, help="端到端延迟中位数上限 ms（含等于）")
    parser.add_argument("--min-download-mib-s", type=float, default=15, help="完整下载速度中位数下限 MiB/s（含等于）")
    parser.add_argument("--origin-code", type=int, default=404, help="域名根路径预期状态码")
    parser.add_argument("--out", type=Path, default=STATE / "results", help="无凭据结果目录")
    allowed_settings = {"label", "iface", "threads", "shortlist", "top", "reps", "download_mib", "download_timeout", "origin_code",
                        "max_latency_ms", "min_download_mib_s"}
    parser.set_defaults(**{k: v for k, v in settings.items() if k in allowed_settings})
    args = parser.parse_args()
    for key in ("threads", "shortlist", "top", "reps", "download_mib", "download_timeout"):
        if not isinstance(getattr(args, key), int) or getattr(args, key) < 1:
            parser.error(f"--{key.replace('_', '-')} 必须大于零")
    if args.threads > 512:
        parser.error("--threads 最多 512")
    for key in ("max_latency_ms", "min_download_mib_s"):
        value = getattr(args, key)
        if not isinstance(value, (int, float)) or not math.isfinite(value) or value < 0 or (key == "max_latency_ms" and value == 0):
            parser.error(f"--{key.replace('_', '-')} 必须是有效的{'正数' if key == 'max_latency_ms' else '非负数'}")
    if not shutil.which("curl"):
        raise ValueError("需要系统 curl")
    node = load_node(args, config)
    if args.bind or args.list_nodes:
        return 0
    iface = physical_interface(args.iface)
    binary = find_mihomo(args)
    ips, controls = candidates(args, node, iface)
    report = {"started": time.strftime("%Y-%m-%d %H:%M:%S %z"), "network_label": args.label,
              "node_name": node.get("name", "CF"), "domain": node["servername"],
              "host": node["xhttp-opts"].get("host") or node["servername"], "port": node["port"],
              "download_bytes_per_round": args.download_mib * 1048576,
              "download_chunk_bytes": DOWNLOAD_CHUNK_BYTES, "download_timeout": args.download_timeout,
              "limits": {"max_latency_ms": args.max_latency_ms, "min_download_mib_s": args.min_download_mib_s},
              "interface": iface, "tcp_scanned": len(ips), "domain_checks": [], "runs": {}, "summary": []}
    print(f"环境 {args.label}；节点 {node['servername']}:{node['port']}，网卡 {iface}；仅测 CF。", flush=True)
    print(f"推荐条件：延迟中位数 ≤{args.max_latency_ms:g} ms，下载速度中位数 ≥{args.min_download_mib_s:g} MiB/s，全部轮次完整下载成功。", flush=True)
    print("全部未达标时，取全部轮次下载成功的入口中速度最高者；同速优先低延迟。", flush=True)
    print(f"1/3 TCP 初筛：{len(ips)} 个入口…", flush=True)
    tcp = []
    with concurrent.futures.ThreadPoolExecutor(args.threads) as pool:
        futures = {pool.submit(tcp_probe, ip, node["port"], iface): ip for ip in ips}
        for done, future in enumerate(concurrent.futures.as_completed(futures), 1):
            value = future.result()
            if value is not None:
                tcp.append((value, futures[future]))
            if done % 500 == 0 or done == len(ips):
                print(f"  {done}/{len(ips)}，TCP 可达 {len(tcp)}", flush=True)
    selected = list(dict.fromkeys([ip for _, ip in sorted(tcp)[:args.shortlist]] + controls))
    print(f"2/3 原域名回源筛选：{len(selected)} 个 IP，预期 HTTP {args.origin_code}…", flush=True)
    passed = []
    with concurrent.futures.ThreadPoolExecutor(min(args.threads, 32)) as pool:
        futures = {pool.submit(domain_probe, ip, node, iface, args.origin_code): ip for ip in selected}
        for done, future in enumerate(concurrent.futures.as_completed(futures), 1):
            result = future.result()
            report["domain_checks"].append(result)
            if result["pass"]:
                passed.append(result)
            if len(selected) <= 20 or result["ip"] in controls:
                status = "通过" if result["pass"] else "淘汰"
                reason = f" CF {result['edge_error']}" if result["edge_error"] else ""
                print(f"  {result['ip']:16} HTTP {result['status']} {status}{reason}", flush=True)
            elif done % 50 == 0:
                print(f"  {done}/{len(selected)}，回源通过 {len(passed)}", flush=True)
    test_ips = [r["ip"] for r in sorted(passed, key=lambda r: r["seconds"])[:args.top]]
    test_ips = list(dict.fromkeys(test_ips + [r["ip"] for r in passed if r["ip"] in controls]))
    if test_ips:
        size = args.download_mib * 1048576
        print(f"3/3 实际 XHTTP：{len(test_ips)} 个 IP × {args.reps} 轮 × {args.download_mib} MiB；"
              f"计划下载约 {len(test_ips) * args.reps * args.download_mib} MiB。", flush=True)
        print(f"  单请求最多 {DOWNLOAD_CHUNK_BYTES // 1048576} MiB，每轮总下载限时 {args.download_timeout} 秒。", flush=True)
        with Core(binary, node, test_ips, iface) as core:
            for rep in range(args.reps):
                order = list(enumerate(test_ips))
                random.shuffle(order)
                for index, ip in order:
                    result = core.measure(index, size, args.download_timeout)
                    report["runs"].setdefault(ip, []).append(result)
                    speed = f"{result['download_mib_s']:.2f} MiB/s" if result["download_ok"] else result.get("error", "失败")
                    print(f"  第 {rep + 1} 轮 {ip:16} 延迟 {result['latency_ms']} ms，{speed}", flush=True)
        report["summary"] = [qualify(summarize(ip, runs, ip in controls), args.max_latency_ms, args.min_download_mib_s)
                             for ip, runs in report["runs"].items()]
        report["summary"].sort(key=lambda r: (r["eligible"], r["success"] / r["rounds"], r["median_download_mib_s"] or 0,
                                                -(r["median_latency_ms"] or 1e9)), reverse=True)
    else:
        print("没有通过原域名筛选的 IP；未启动代理测速。", flush=True)
    winner = choose_recommendation(report["summary"])
    report["recommendation"] = winner
    for row in report["summary"]:
        row["recommended"] = bool(winner and row["ip"] == winner["ip"])
    report["finished"] = time.strftime("%Y-%m-%d %H:%M:%S %z")
    args.out.mkdir(parents=True, exist_ok=True, mode=0o700)
    stamp = time.strftime("%Y%m%d-%H%M%S") + f"-{os.getpid()}"
    json_path, csv_path = args.out / f"cf-{stamp}.json", args.out / f"cf-{stamp}.csv"
    json_path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    json_path.chmod(0o600)
    fields = ["ip", "success", "rounds", "median_latency_ms", "median_download_mib_s", "control", "eligible", "rejection_reason", "recommended"]
    with csv_path.open("w", encoding="utf-8-sig", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fields)
        writer.writeheader()
        writer.writerows(report["summary"])
    csv_path.chmod(0o600)
    print("\nIP               成功轮数  延迟中位数(ms)  下载中位数(MiB/s)  达标情况")
    for row in report["summary"]:
        latency = row['median_latency_ms'] if row['median_latency_ms'] is not None else "—"
        speed = row['median_download_mib_s'] if row['median_download_mib_s'] is not None else "—"
        print(f"{row['ip']:16} {row['success']}/{row['rounds']:<6} {str(latency):>12} "
              f"{str(speed):>18}  " + ("达标" if row["eligible"] else row["rejection_reason"]) + ("  对照" if row["control"] else ""))
    if winner:
        if winner["fallback"]:
            print(f"\n没有达标入口，退而选择下载速度最高者（未达标：{winner['rejection_reason']}）。")
        print(f"\n本轮推荐入口：{winner['ip']}:{node['port']}；SNI / Host 保持原值。")
    else:
        print("\n本轮没有全部轮次完整下载成功且有有效速度的入口，无法推荐。")
    print(f"结果：{csv_path}\n详情：{json_path}\n临时内核已停止，含凭据配置已清理。")
    return 0 if winner else 1


def interrupted(signum, frame):
    raise KeyboardInterrupt


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, interrupted)
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        print("\n测速已中止，临时内核和凭据配置已清理。", file=sys.stderr)
        raise SystemExit(130)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        # Do not expose raw config/link strings in exception output.
        print(f"测速失败：{type(error).__name__}。" + (str(error) if type(error) is ValueError else
              "请检查网络、文件路径或参数。"), file=sys.stderr)
        raise SystemExit(2)
