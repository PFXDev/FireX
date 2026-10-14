# 用户侧独立 CF XHTTP 测速工具

FireX 部署在境外核心服务器，只提供 Clash / mihomo 格式的订阅。测速工具运行在用户侧的 macOS / Linux 网络环境，通过 HTTP(S) 拉取订阅，不访问 FireX 数据库、不依赖 FireX 项目目录或 FlClash。

给每个用户环境分发 `cf-speedtest-client.zip` 并解压。在解压目录运行：

```sh
./cf-speedtest.command --bind --label "上海移动"
```

输入可从该网络访问的订阅 URL，然后从列表中选择 CF XHTTP 节点。URL 输入不回显，避免留在终端输出或命令历史里。如果只有一个可测速节点，会自动选中。绑定完成后写入本机配置文件，下一次一键测速：

```sh
./cf-speedtest.command
```

macOS 也可以双击 `.command` 文件。第一次运行且没有绑定时会引导输入订阅、选择节点和填写网络环境名称，再开始测速。每次测速重新获取订阅，按保存的节点名称使用最新参数；节点消失时会停止并要求重新选择。

客户端依赖 Python 3.10+、curl、PyYAML 和 mihomo。启动器会在本机独立虚拟环境中安装 PyYAML，不修改系统 Python；mihomo 优先使用 PATH 或工具目录中的已有内核，否则从 MetaCubeX 官方发布下载 v1.19.32，并校验发布资源的 SHA-256。Linux 需已有 Python venv 支持。

## 本机配置与跨网络使用

默认本机状态目录是 `~/.config/cf-speedtest/`：

- `config.json`：订阅链接、所选节点名称和测速设置，文件权限 600。
- `results/`：CSV 排名与 JSON 详情，包含网络环境名称，便于汇总多地结果。
- `bin/`、`venv/`：独立内核和 Python 依赖。

每台客户端独立保存配置。订阅地址可使用部署后的公网域名，不要求 FireX 在本机。`localhost` 仅用于本地调试，不能作为其他客户端访问境外 FireX 的地址。

更换订阅或节点：

```sh
./cf-speedtest.command --bind --label "杭州电信"
./cf-speedtest.command --list-nodes
./cf-speedtest.command --node "订阅中的完整节点名称"
```

`--bind` 只保存绑定配置；`--node` 保存节点选择并测速。当前只列出能独立运行、启用证书校验的 VLESS + XHTTP + TLS 节点，REALITY 不适用 CF 优选。

自动识别出站物理网卡，检测到 TUN 网卡会停止；可使用 `--iface en0` 显式绑定 macOS 物理网卡。Linux 若没有绑定网卡的权限会停止并提示。工具只监听回环地址，不启用 TUN、不修改系统代理、DNS 或服务器节点配置。

若需要同一台设备保存多个网络配置：

```sh
./cf-speedtest.command --config ./mobile.json --bind --label "上海移动"
./cf-speedtest.command --config ./mobile.json
./cf-speedtest.command --config ./telecom.json --bind --label "上海电信"
./cf-speedtest.command --config ./telecom.json
```

也可直接编辑配置文件，结构见包内 `cf-speedtest-config.example.json`。用 `CF_SPEEDTEST_HOME=/path/to/state` 更换整个状态目录；用 `--out /path/to/results` 更换结果目录。配置中只保存 URL 与节点名称，不缓存 UUID；订阅链接本身有访问权限，请保持配置权限 600。

## 测试方法

1. 从 Cloudflare 官方 IPv4 网段每个 /24 抽一个 IP，用所选节点端口做 TCP 初筛。
2. 使用所选节点的原 SNI / HTTP Host 请求域名根路径。默认预期 HTTP 404，403 / CF 1034 会被淘汰；其他节点用 `--origin-code` 设置预期状态码。
3. 启动临时 mihomo，通过实际 VLESS + XHTTP 代理测延迟和下载。默认五个候选，加当前入口和域名 DNS 对照，随机顺序测三轮，每轮完整下载 16 MiB。每次请求最多 8 MiB，较大下载分多次请求完成，速度按总下载字节除以全部请求的实际总耗时计算。保留订阅里的 TLS、UUID、XHTTP、加密等参数，仅替换候选连接 IP 并绑定物理网卡。

默认推荐条件是：**实际 XHTTP 延迟中位数 ≤190 ms、完整下载速度中位数 ≥15 MiB/s，且全部轮次下载成功**。两个阈值都包含等于。优先选择达标入口中下载速度最高者；全部未达标时，取全部轮次下载成功的入口中下载速度最高者，并明确标记为未达标的备选。同速优先低延迟，没有完整成功的入口则不推荐。CSV 的 `recommended` 标记最终选择，JSON 的 `recommendation.fallback` 标明是否采用备选；保留原达标状态和原因。阈值针对实际代理表现，TCP 和域名根路径请求仅用于初筛。

可临时覆盖阈值，或在 `--bind` 时保存到配置：

```sh
./cf-speedtest.command --max-latency-ms 190 --min-download-mib-s 15
```

配置字段为 `settings.max_latency_ms`、`settings.min_download_mib_s`。错误响应及未完成下载不会计为有效速度。下载测试站目前拒绝单次 16 MiB 请求（HTTP 403），分小请求可避免这一已复现的问题；默认 25 秒是整轮下载的限时。失败时显示 HTTP 状态、已收字节或 curl 退出码，JSON 记录每个请求的详情。慢网络可使用 `--download-timeout 60` 延长整轮限时。

导入已有 cfst CSV，跳过官方网段扫描：

```sh
./cf-speedtest.command --csv ./result.csv
```

只测指定 IP，另加当前入口及 DNS 对照：

```sh
./cf-speedtest.command --ips 162.159.39.148 162.159.45.39 104.17.26.239
```

调整测试量，或者在绑定时把这些参数保存为默认值：

```sh
./cf-speedtest.command --top 10 --reps 3 --download-mib 32
./cf-speedtest.command --bind --label "北京联通" --top 10 --reps 3 --download-mib 32
```

其他参数：`--mihomo /path/mihomo`、`--ip-file ip.txt`、`--help`。`--ip-file` 每行可放 IPv4 或 CIDR，每个 CIDR 抽一个地址。`--node-json /path/node.json` 可临时使用单个 mihomo 节点对象，不更改订阅绑定。

CSV 是排序汇总，JSON 包含网络名称、节点名称、筛选结果和每轮详情。速度单位是 MiB/s。结果不含 UUID、密钥或订阅 URL；临时内核与凭据配置在结束、报错或 Ctrl+C 时清理。

多网络对比时使用相同节点、下载大小和轮数，白天及晚高峰各测一次。推荐仅代表本次用户网络到 CF 再到源站的实际表现，不自动更改 FireX 或其他客户端节点。
