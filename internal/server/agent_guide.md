# FireX Agent API 指南

FireX 是一套 3x-ui 面板集群的控制面。你通过本 API 读取它的配置快照，修改后提交；
**提交不会立即生效**，由管理员在网页上审核差异后合并。

所有请求都带 `Authorization: Bearer <令牌>`。令牌只属于这一次会话：合并、取消或过期后立即失效。

## 接口

| 方法 | 路径 | 作用 |
| ---- | ---- | ---- |
| GET  | `/api/agent` | 会话状态、任务、管理员的退回意见 |
| GET  | `/api/agent/guide` | 本指南 |
| GET  | `/api/agent/snapshot` | 当前配置快照，含 `revision` |
| POST | `/api/agent/preview` | 自检：校验并返回差异，不写入、不提交，可反复调用 |
| PUT  | `/api/agent/submission` | 提交给管理员审核；可多次提交，新的覆盖旧的 |

## 工作流程

1. `GET /api/agent/snapshot`，保存返回的整个 JSON，记下 `revision`。
2. 只修改与任务相关的部分。
3. `POST /api/agent/preview`，请求体是 `{ "revision": "...", ...要提交的块 }`。检查返回的
   `changes` 是否只包含你想要的改动。报错时按错误信息修正。
4. `PUT /api/agent/submission`，请求体同上，外加 `"summary"`：用几句话说明改了什么、为什么。
5. 轮询 `GET /api/agent`（建议间隔 10 秒以上）：
   - `submitted`：等待审核。
   - `returned`：被退回。`feedback` 是管理员意见。**重新读取快照**，按意见修改后再次提交。
   - `merged`：已合并，任务完成，令牌失效。
   - 返回 401：会话已结束、取消或过期，停止操作。

`revision` 在提交和合并时都会校验。如果期间配置被别人改过，提交会被拒绝（409），或者在合并时被自动退回。
此时重新读取快照，基于最新内容重做。

## 快照规则

- **块可选，带上的块就是最终状态。** 顶层块有 `panels`、`inbounds`、`inboundTemplates`、
  `nodeGroups`、`profiles`、`routing`、`clashTemplate`。不带某个块，该块就不会被改动。
  带了某个块，就必须写出它的全部条目：没列出的条目会被删除，条目里省略的字段取新建时的默认值。
  所以最稳妥的做法是：从读到的快照出发修改，只提交改动涉及的块，并且保留这些块的其余内容原样。
- **一律按名称引用**，不用数据库 ID。入站写作 `{ "panel": "面板名", "remoteId": 3 }`；
  节点组、分流方案、分流策略、规则集都按裸名称（不含 emoji 图标）引用。
- **整份校验**。整份文档在一个事务里校验，任何一处出错都整体拒绝，错误信息会指出位置。

## 各块说明

- `panels`：3x-ui 面板。导出时不含 `apiToken`。新面板必须提供 `apiToken`；已有面板留空表示保持不变。
  删除面板会删掉其上全部 FireX 客户端，需要管理员在合并时额外确认。新面板的入站要合并之后才会被发现，
  所以同一次提交里不能引用它的入站。
- `inbounds`：入站由面板发现，不能新建。`tag`、`protocol`、`port`、`remoteRemark`、`missing`
  只供阅读，提交时会被忽略。可改的有 `name`、`emoji`、`sortOrder`、`enabled`、`udp`、`vision`、
  `xhttpTuning`、`publicAddress`、`publicPort`。`managedConfig` 是 FireX 接管的面板入站配置，
  设为 `null` 表示不再接管。改动后的配置只会变成「待下发」，不会写到面板。
  面板上仍然存在的入站不能从列表里删掉。
- `inboundTemplates`：入站模板，`{ name, parameters }`。
- `nodeGroups`：节点组。一个节点组把手选的入站组成一个客户端代理组，字段有 `type`（`select`、`url-test`、
  `fallback`、`load-balance`）、`tags`（键值对，如 `地区`、`线路`）、`inbounds`（入站引用列表）。
- `profiles`：分流方案，相当于一个档位。`nodeGroups` 是白名单，决定该档用户能用哪些入站；
  `allGroups: true` 表示放行全部节点组。还有套餐绑定的方案不能删除。
- `routing`：分流矩阵。
  - `policies` 是分流策略，**顺序就是规则优先级**，也是客户端里策略组的显示顺序。必须恰好有一条
    `isFinal: true` 的策略，它渲染为 `MATCH`，排在最后且不带规则。
  - `rules` 的写法如 `{ "type": "DOMAIN-SUFFIX", "value": "openai.com" }`；`RULE-SET` 类型的
    `value` 是规则集的名称。
  - `egresses` 是矩阵的格子：`policy` 填策略名，`profile` 为空表示默认列。`members` 按顺序列出，
    `kind` 取值有 `node-group`、`policy`、`builtin`（`DIRECT`、`REJECT` 等）、`all-node-groups`
    （展开为该方案白名单里的全部节点组）、`all-inbounds`。`hidden: true` 表示该方案不输出这条策略。
    方案列只写和默认列不同的格子。
  - `ruleSets` 是规则集，供 `RULE-SET` 规则引用；省略这个键表示规则集保持不变。
- `clashTemplate`：mihomo 基础模板，只管 DNS、嗅探、端口；空字符串表示使用内置默认模板。
  `proxies`、`proxy-groups`、`rules` 始终由矩阵生成。

## 不在快照里的

用户、套餐、流量和订阅令牌不在快照里，无法通过本 API 修改。
