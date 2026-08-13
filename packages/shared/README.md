# packages/shared · 跨端共享契约

> Web / API / Worker 三端共享的单一事实来源（Single Source of Truth）：
> 错误码、链注册表、事件 schema、OpenAPI 片段。所有跨端契约都必须**先在这里
> 演进**，再被 web / api / worker 各自消费；不允许在组件 / handler / 函数里
> 内联重复定义。
>
> 全局架构、产品流程、合约地址与 AWS 拓扑统一在顶层
> [README.md](../../README.md) 维护。本文件覆盖本包自身的结构、每个模块的
> 不变量、跨端使用约束与扩展示例。

---

## 1. 模块定位

本包刻意保持「薄」——只放**类型与契约**，不放业务逻辑、不放 HTTP 工具、
不放数据库访问。

| 子模块 | 内容 | 消费者 |
|---|---|---|
| `src/errors/` | 错误码枚举 + 默认 HTTP status 映射 | web（`api/client.ts`）、API（`errcode/codes.go`）|
| `src/chains/` | 链 registry（Anvil / Sepolia）| web（`src/chains.ts`）、worker（chainID 校验）|
| `src/events/` | 事件 schema（CourseMarket / YDToken / CertificateNFT）+ topic0 | worker（`internal/chain` 解码）、web（log 解析 / 测试）|
| `src/openapi/` | 部分 OpenAPI 片段（auth / course）| 内部 TS 类型生成（web 测试）|
| `openapi/`（仓库根旁） | 完整 OpenAPI 文档（admin / certificate / chain-sync / course / errors / learning / order）| API 端由 `scripts/check-openapi.mjs` 校验 |

`package.json` 用 `exports` 暴露子路径，便于只导入需要的部分：

```jsonc
"exports": {
    ".": "./src/index.ts",
    "./openapi": "./src/openapi/index.ts",
    "./errors": "./src/errors/index.ts",
    "./events": "./src/events/index.ts",
    "./chains": "./src/chains/index.ts"
}
```

web 端依赖示例（[apps/web/package.json](../../apps/web/package.json)）：

```jsonc
"dependencies": {
    "@x-web3/shared": "workspace:*",
    ...
}
```

---

## 2. 目录结构

```text
packages/shared/
├── package.json                        # @x-web3/shared · workspace 包
├── tsconfig.json                       # ESM + strict（与 web 一致）
├── src/
│   ├── index.ts                        # 总入口（errors / chains / events）
│   ├── chains/
│   │   ├── index.ts                    # re-export registry + helpers
│   │   └── registry.ts                 # CHAINS map + getChain()
│   ├── errors/
│   │   ├── index.ts                    # re-export ErrorCode + ApiError + ErrorHttpStatus
│   │   └── codes.ts                    # 枚举 + 默认 status 映射
│   ├── events/
│   │   ├── index.ts                    # re-export 所有事件类型 + topic0
│   │   ├── primitives.ts               # Address / Hash / Hex / 基础类型
│   │   ├── courseMarket.ts             # CourseConfigured / CoursePurchased + topic0
│   │   ├── ydToken.ts                  # Transfer / Approval / Minted / CapSet + topic0
│   │   └── certificateNft.ts           # Transfer / Approval / CertificateMinted + topic0
│   └── openapi/
│       ├── index.ts                    # 子路径入口
│       ├── auth.yaml                   # 鉴权相关片段
│       └── course.yaml                 # 课程相关片段
└── openapi/                            # 仓库根 OpenAPI 文档
    ├── admin.yaml · certificate.yaml · chain-sync.yaml
    ├── course.yaml · errors.yaml · learning.yaml · order.yaml
```

---

## 3. 错误码（`src/errors/codes.ts`）

### 3.1 命名约定

`<DOMAIN>_<REASON>` 全大写下划线：

```ts
INVALID_PRIVY_TOKEN          // 身份 / 鉴权
WALLET_ALREADY_BOUND        // 身份 / 鉴权
COMMENT_NOT_PURCHASED       // 课程
INTENT_EXPIRED              // 订单 / 链
PRICE_VERSION_MISMATCH      // 订单 / 链
PROGRESS_REGRESSION         // 学习 / 证书
CERTIFICATE_DUPLICATE       // 学习 / 证书
CONFIRM_TOKEN_INVALID       // 权限 / 管理
```

### 3.2 完整列表（按域分组）

| 域 | 错误码 |
|---|---|
| 通用 | `BAD_REQUEST / UNAUTHORIZED / FORBIDDEN / NOT_FOUND / CONFLICT / INTERNAL / RATE_LIMITED` |
| 身份 / 鉴权 | `INVALID_PRIVY_TOKEN / PRIVY_TOKEN_EXPIRED / SESSION_EXPIRED / WALLET_ALREADY_BOUND / WALLET_SIGNATURE_INVALID / WALLET_NONCE_REUSED / CANNOT_UNBIND_LAST_WALLET / ROLE_CHANGE_REQUIRES_CONFIRM` |
| 课程 | `COURSE_STATE_CONFLICT / STALE_VERSION / NOT_ENROLLED / COMMENT_NOT_PURCHASED / MEDIA_CHECKSUM_MISMATCH / MEDIA_NOT_READY` |
| 订单 / 链 | `INTENT_EXPIRED / PRICE_VERSION_MISMATCH / INVALID_TX_RECEIPT / ALREADY_PURCHASED / EVENT_REORGED / RPC_UNAVAILABLE / WRONG_CHAIN / INSUFFICIENT_ALLOWANCE` |
| 学习 / 证书 | `PROGRESS_REGRESSION / ALREADY_COMPLETED / CERTIFICATE_DUPLICATE / MINT_NOT_AUTHORIZED / MINT_FAILED` |
| 权限 / 管理 | `CONFIRM_TOKEN_INVALID / CHAIN_REPLAY_OUT_OF_RANGE` |

### 3.3 默认 HTTP status

`ErrorHttpStatus: Record<ErrorCodeValue, number>` 提供**参考**映射；后端
实际可按情况调整（如部分 `409` 用 `412`/`422`），前端按 `code` 而非
status 决定 UX，避免耦合。

### 3.4 `ApiError` 类型

```ts
export interface ApiError {
  code: string;          // 来自 ErrorCode 枚举
  message: string;       // 面向用户；可本地化
  requestId: string;     // 与响应头 X-Request-ID 一致
  details?: Record<string, unknown>;
}
```

后端 envelope（[apps/api/internal/httpkit/router.go](../../apps/api/internal/httpkit/router.go)）
与前端 [apps/web/src/api/client.ts](../../apps/web/src/api/client.ts) 共享这个形状。

### 3.5 使用约束

- **禁止**在组件 / handler 内联字符串错误码；
- 后端拿到业务错误时用 `errcode.New(code, message, details)`；
- 前端 `ApiClientError.code` 用作 `switch` 关键字（const enums 在 `isolatedModules`
  下不可用）。

---

## 4. 链 registry（`src/chains/registry.ts`）

```ts
export interface ChainInfo {
    namespace: 'eip155';
    chainId: number;
    name: string;
    shortName: string;
    rpcEnvVar: string;      // 从 import.meta.env / process.env 读取的 env 名
    blockExplorer: string;
    nativeToken: 'ETH';
    isTestnet: boolean;
    confirmationDepth: number;
}

export const CHAINS: Record<number, ChainInfo> = {
    31337:    { name: 'Anvil',   shortName: 'anvil',   rpcEnvVar: 'VITE_ANVIL_RPC_URL',   blockExplorer: '',                          ... },
    11155111: { name: 'Sepolia', shortName: 'sepolia', rpcEnvVar: 'VITE_SEPOLIA_RPC_URL', blockExplorer: 'https://sepolia.etherscan.io', ... },
};
```

### 4.1 关键不变量

- **`rpcEnvVar` 是字符串而非真实 URL**：避免在客户端 bundle 出现 RPC key。
- `confirmationDepth` 与 worker 的 `CHAIN_CONFIRMATION_DEPTH` 同源；改值必须
  两边对齐。
- 不支持 Ethereum 主网（`isTestnet = true` 始终）；加新链必须先在
  [docs/adr/0001-production-target-chain.md](../../docs/adr/0001-production-target-chain.md) 决议。

### 4.2 扩展流程

1. 决议通过后，在 `CHAINS` 里加一条；
3. 同步 worker env（`WORKER_CHAIN_ID`）；
4. 同步 API env（`CHAIN_ID`）；
5. 跑 [scripts/check-openapi.mjs](../../scripts/check-openapi.mjs) 与
   `pnpm typecheck -r`。

---

## 5. 事件 schema（`src/events/`）

### 5.1 设计原则

- **字段命名严格匹配 Solidity event**（参见合约
  [CourseMarket.sol](../../packages/contracts/src/CourseMarket.sol)），
  避免 worker / 合约之间漂移；
- 大整数一律 `bigint`（不是 `number`），因为 `uint256` 会溢出 JS Number；
- 地址 / hash / bytes 一律用 `viem` 风格的字面量类型 `\`0x${string}\``；
- topic0 是事件签名的 `keccak256` 前 4 字节 hex；worker 解码时与 `chain.Decode` 对齐。

### 5.2 `CourseMarket` 事件

```ts
export interface CourseConfiguredEvent {
    courseKey: `0x${string}`;     // bytes32
    token: Address;
    amount: bigint;               // uint256
    priceVersion: bigint;
}

export interface CoursePurchasedEvent {
    courseKey: `0x${string}`;
    buyer: Address;               // indexed
    token: Address;
    amount: bigint;
    intentId: `0x${string}`;      // bytes16 hex (32 hex chars)
    priceVersion: bigint;
}

export const COURSE_MARKET_EVENT_SIGNATURES = {
    CourseConfigured: '0x7c4bd32c23ea1943334ebe7040a4294f81f2b76a6c27bfc63245e86971ff9264',
    CoursePurchased:  '0xee2c004361a941cef00dd638a722b034a58392d57a99eab2617793b17a6005ad',
} as const;
```

### 5.3 `DecodedLog<T>` — Worker 入库前最小校验结构

```ts
export interface DecodedLog<T> {
    chainId: number;
    blockNumber: bigint;
    blockHash: Hash;
    txHash: Hash;
    logIndex: number;
    address: Address;             // 触发合约地址（= market）
    event: T;
    rawTopics: readonly Hex[];
    rawData: Hex;
}
```

worker `internal/order/confirmer.go::Apply` 入参的 `Event` 字段就是
`DecodedLog<CoursePurchasedEvent>['event']` 的等价结构。

### 5.4 `YDToken` 与 `CertificateNFT`

- YDToken：`Transfer / Approval / RoleGranted / RoleRevoked / Paused / Unpaused
  / CapSet / Minted`，topic0 在 `YD_TOKEN_EVENT_SIGNATURES`；
- CertificateNFT：`Transfer / Approval / ApprovalForAll / RoleGranted /
  RoleRevoked / CertificateMinted`，topic0 在 `CERTIFICATE_NFT_EVENT_SIGNATURES`。

导出时使用 alias 解决 ERC20 / ERC721 同名冲突（`TransferEvent20` /
`TransferEvent721`）。

### 5.5 topic0 计算与漂移防御

- `topic0 = keccak256("EventName(type,...)")` 取前 4 字节 hex；
- 离线工具：[packages/contracts/script/compute-topics.mjs](../../packages/contracts/script/compute-topics.mjs)；
- 任何合约 event 改动**必须**：
  1. 改合约源码；
  2. `forge build` + `pnpm contracts:export:abi`；
  3. 同步这里的 `interface + SIGNATURES`；
  4. 在 worker 的 [internal/chain/decoder_test.go](../../apps/worker/internal/chain/decoder_test.go) 加断言。

---

## 6. OpenAPI（`src/openapi/` + `openapi/`）

### 6.1 两套用途

- **`src/openapi/*.yaml`**：被 web / 共享类型生成器消费的片段（auth / course）；
  目前是占位 + 轻量定义，供类型扩展（如未来引入 `openapi-typescript`）。
- **`openapi/*.yaml`**（仓库根）：完整 API 文档（admin / certificate /
  chain-sync / course / errors / learning / order / auth）。
  - 由 `pnpm openapi:check` 校验（[scripts/check-openapi.mjs](../../scripts/check-openapi.mjs)）；
  - 是 API handler / web 端类型的**事实来源**。

### 6.2 错误信封契约在 `openapi/errors.yaml`

```yaml
# 通用错误响应
ApiError:
  type: object
  required: [code, message, requestId]
  properties:
    code:        { type: string, example: 'INTENT_EXPIRED' }
    message:     { type: string }
    requestId:   { type: string }
    details:
      type: object
      additionalProperties: true
```

任何修改都会让 API envelope 与前端 `ApiClientError` 失配，必须三端同步。

### 6.3 校验

```bash
pnpm openapi:check   # 跑 scripts/check-openapi.mjs
```

CI 期望 0 error；新增 endpoint 必须先在对应 YAML 里加 schema，再写 handler。

---

## 7. 与三端的契约

### 7.1 Web（[apps/web](../../apps/web/)）

- 通过 `@x-web3/shared` 子路径导入；
- `ErrorCode` 用于 `switch (error.code)`；
- `CHAINS` 与 `chains.ts` 中的 `targetChain` 同步；
- 事件 schema 用作 log 解析与单测断言。

### 7.2 API（[apps/api](../../apps/api/)）

- Go 端通过 [scripts/check-openapi.mjs](../../scripts/check-openapi.mjs) 校验
  OpenAPI 与 Go handler 名字一致；
- 错误码 SSOT 是这里的 `ErrorCode`；Go 端
  [internal/errcode/codes.go](../../apps/api/internal/errcode/codes.go) 必须保持镜像同步。
- 共享 envelope 形状（`{ error: { code, message, requestId, details? } }`）。

### 7.3 Worker（[apps/worker](../../apps/worker/)）

- 通过 topic0（`chain.CoursePurchasedTopic`）解码事件；
- Go 端有自己的事件类型，但 schema 与这里的 TypeScript 接口**必须**一致
  （字段名 / 类型 / 顺序）。
- 链 ID 与 `CHAINS[chainId].confirmationDepth` 对齐；worker 端的
  `CHAIN_CONFIRMATION_DEPTH` env 默认 12（Sepolia）。

---

## 8. 扩展指南

### 8.1 新增错误码

1. 在 `src/errors/codes.ts` 加一行；
2. 在 `ErrorHttpStatus` 里加默认 status；
3. 在 `openapi/errors.yaml` 加 schema 描述（如必要）；
4. 后端 `apps/api/internal/errcode/codes.go` 镜像同步；
5. 前端在用到该 code 的组件里 `switch (error.code)` 处理；
6. 跑 `pnpm openapi:check` 与 `pnpm typecheck -r`。

### 8.2 新增链

1. 在 [docs/adr/](../../docs/adr/) 立 ADR；
2. 在 `src/chains/registry.ts` 加 `ChainInfo`；
3. 同步 worker / API 的 env 默认值；
4. 在 [docs/DEPLOYMENTS.md](../../docs/DEPLOYMENTS.md) 留部署档案占位；
6. 在合约包 [packages/contracts](../../packages/contracts) 加 `--rpc-url` profile。

### 8.3 新增事件

1. 改合约源码 + `forge build` + `pnpm contracts:export:abi`；
2. 在 `src/events/<contract>.ts` 加 `interface` + topic0（用 `compute-topics.mjs`
   校验）；
3. 在 `src/events/index.ts` 加 re-export；
4. worker `internal/chain/decoder_test.go` 加断言；
5. `pnpm openapi:check` + `pnpm typecheck -r`。

---

## 9. 版本与发布

- 当前版本：`0.1.0`，`private: true`，不发布到 npm；
- 通过 pnpm workspace 让 `apps/web` 直接以 `@x-web3/shared` 引用；
- 任何 breaking 改动必须先把三端统一升级，再合 main。

---

## 10. 进一步阅读

- 全局架构：[docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md)
- 产品流程：[docs/PRODUCT-FLOWS.md](../../docs/PRODUCT-FLOWS.md)
- ADR 总览：[docs/adr/](../../docs/adr/)
- OpenAPI 校验：[scripts/check-openapi.mjs](../../scripts/check-openapi.mjs)
- 错误信封源：[apps/api/internal/httpkit/router.go](../../apps/api/internal/httpkit/router.go)
- 前端消费示例：[apps/web/src/api/client.ts](../../apps/web/src/api/client.ts)
- Worker 解码：[apps/worker/internal/chain/decoder.go](../../apps/worker/internal/chain/decoder.go)
- 合约：[packages/contracts](../contracts/README.md)
- 前端：[apps/web](../../apps/web/README.md)
- API：[apps/api](../../apps/api/README.md)
- Worker：[apps/worker](../../apps/worker/README.md)