/** Chinese labels for Xray inbound parameter keys, shared by the read-only view and the editor. */
export const parameterLabels: Record<string, string> = {
  id: '入站 ID', tag: '入站标签', remark: '面板备注', protocol: '协议',
  listen: '监听地址', port: '监听端口', enable: '启用', enabled: '启用',
  network: '传输方式', security: '安全方式', settings: '协议设置',
  streamSettings: '传输与安全', sniffing: '流量嗅探', acceptProxyProtocol: '接收 PROXY 协议',
  realitySettings: 'REALITY', tlsSettings: 'TLS', tcpSettings: 'TCP', rawSettings: 'RAW',
  wsSettings: 'WebSocket', grpcSettings: 'gRPC', xhttpSettings: 'XHTTP',
  httpSettings: 'HTTP', httpupgradeSettings: 'HTTP Upgrade', sockopt: 'Socket 选项',
  serverNames: '服务器名称 / SNI', serverName: '服务器名称 / SNI', dest: '目标地址', target: '目标地址',
  privateKey: '私钥', publicKey: '公钥', shortIds: 'Short IDs', shortId: 'Short ID',
  fingerprint: '指纹', spiderX: 'Spider X', show: '显示调试信息',
  xver: 'Proxy Protocol 版本', maxTimeDiff: '最大时间差', minClientVer: '最低客户端版本',
  maxClientVer: '最高客户端版本', alpn: 'ALPN', certificates: '证书', certificate: '证书内容',
  certificateFile: '证书路径', keyFile: '私钥路径', key: '私钥内容',
  allowInsecure: '允许不安全证书', rejectUnknownSni: '拒绝未知 SNI',
  decryption: '解密', encryption: '加密', fallbacks: '回落规则',
  limitFallbackUpload: '回落上传限速', limitFallbackDownload: '回落下载限速',
  afterBytes: '阈值字节数', bytesPerSec: '每秒字节数', burstBytesPerSec: '突发速率',
  path: '路径', host: 'Host', headers: '请求头', header: '传输头', type: '类型', serviceName: '服务名称',
  multiMode: 'Multi mode', mode: '模式', extra: '扩展参数',
  destOverride: '嗅探目标', routeOnly: '仅用于路由', metadataOnly: '仅嗅探元数据',
  domainsExcluded: '排除域名', ipsExcluded: '排除 IP',
  disableFlow: '禁用流控', shareAddr: '分享地址', shareAddrStrategy: '分享地址策略',
  allocate: '连接分配', strategy: '策略',
  tcpFastOpen: 'TCP Fast Open', tcpNoDelay: 'TCP No Delay', tcpCongestion: 'TCP 拥塞控制',
  dialerProxy: '拨号代理', mark: '路由标记', tproxy: '透明代理',
  up: '上传字节', down: '下载字节', total: '配额字节', expiryTime: '到期时间',
}

export function parameterLabel(key: string): string {
  return parameterLabels[key] || key
}
