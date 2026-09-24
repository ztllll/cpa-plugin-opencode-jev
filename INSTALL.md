# 安装说明 — opencode-jev / jev-ask

## 机器角色先分清

| 角色 | 需要装什么 |
|---|---|
| **A. 跑 CPA 的宿主机**（本机 ser5806905059） | CPA 插件 `opencode-jev-v0.1.0.so` + config 注册 |
| **B. 下游 agent 机器**（只调用，不跑 CPA） | `jev-ask` 脚本 + skill 文件 + 一个管理密钥 |

## A. CPA 宿主机安装（只需一次）

```bash
# 1. 放插件
cp dist/opencode-jev-v0.1.0.so <CPA目录>/plugins/linux/amd64/

# 2. CPA config.yaml 的 plugins.configs 下注册（有 store 区块则可省略）
cat >> <CPA目录>/config.yaml <<'YAML'
    opencode-jev:
      enabled: true
      model: jev-1.13        # 可选，默认 jev-1.13
YAML

# 3. 重启并验证
systemctl --user restart cliproxyapi
journalctl --user -u cliproxyapi | grep "opencode-jev"   # 应看到 plugin loaded version=0.1.0
```

前置条件：CPA config 的 `openai-compatibility` 里至少有一个 base-url 含 "opencode"
的条目且 `api-key-entries` 非空（插件自动发现这些 key 做轮询）。Go key 全部过期时
ask 会返回 502，换新 key 即可。

## B. 调用端安装（pi / codex agent 机器）

### 1. 安装命令行工具

```bash
cp jev-ask ~/.local/bin/ && chmod +x ~/.local/bin/jev-ask
```

默认行为：key 从 `/home/pyadmin/cpa-usage-keeper/.env` 的 `CPA_MANAGEMENT_KEY=` 读取，
端点走本机 `http://127.0.0.1:28317`。**其他机器**需要显式指定：

```bash
export CPA_MANAGEMENT_KEY="<CPA管理密钥>"
export JEV_ENDPOINT="<your-CPA-base-URL>"   # 例如自建域名的 https 入口
```

### 2. 安装 skill（让 LLM 自动会用）

```bash
cp -r skill/jev ~/.agents/skills/        # pi 系 agent，新会话自动加载
```

非 pi 系 agent（如 codex），把 skill 内容要点贴进全局 AGENTS.md：

```markdown
## Jev 决策 API
结构化决策（是非判断/分类/评分）用 `jev-ask`：
    jev-ask "状态描述" '{"q1":{"type":"noul","instructions":"..."},
                         "q2":{"type":"choice","instructions":"...","criteria":{"a":"...","b":"..."}},
                         "q3":{"type":"score","instructions":"...","criteria":["低","中","高"]}}'
仅用于判断/分类，不用于生成文本。score 的 criteria（档位数组）必填。
无本机脚本时用 curl 直调：
    POST $JEV_ENDPOINT/v0/management/plugins/opencode-jev/ask
    Authorization: Bearer $CPA_MANAGEMENT_KEY
    body: {"state":"...","questions":{...}}
```

### 3. 验证

```bash
jev-ask "greet" '{"hi":{"type":"noul","instructions":"Is this a greeting?"}}'
# 期望: {"answers":{"hi":{"noul":0.6,...}},"model":"jev-1.13",...}
```

## 题型速查（三类，都是必填 criteria 的规则）

| type | criteria 格式 | 返回 |
|---|---|---|
| `noul` | 不需要 | `{"noul":0.84}`（"是"的概率） |
| `choice` | **必填**，对象映射 `{"选项":"何时选它"}` | `{"choice":"a","confidence":...}` |
| `score` | **必填**，档位数组 `["Calm","Frustrated","Very angry"]` | `{"score":0.88,...}` |

## 模型选择

- `jev-1.13`（默认，走 Go 套餐额度）
- `jev-1.13-free`（限时免费）：调用端 `export JEV_MODEL=jev-1.13-free`
