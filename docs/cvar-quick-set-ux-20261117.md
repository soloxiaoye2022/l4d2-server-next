# 参数一键选值（Quick Set）交互改造 — 2026-11-17

## 1. 需求

用户在两处页面提出同一类诉求：

1. 服务器配置页（`/#/server-config`）：参数详情里已经展示了「0 关闭 / 1 开启」这类取值卡片，但只是静态说明，点了没反应。希望**点击即写入**，直接改掉对应命令行里的值。
2. 参数库弹窗：希望**先点选值，再点添加**，加进去的命令行使用选中的值，而不是固定默认值。
3. 启动项管理页（`/#/server-startup`）的「选择启动项」弹窗：
   - 有候选值的启动项 → 候选值可直接点击选中，再点添加即按选中值添加；
   - 需要填数值的启动项 → 预设几个常用值 + 一个自定义输入框；
   - **互斥**：一旦在自定义输入框里输入，前面选中的预设值自动取消，不允许「既选又填」造成重复；
   - 支持单值内含多个值的参数（如 `l4d2_melee_spawn "pitchfork;shovel"`）。

## 2. 改造前现状取证

| 位置 | 现状 | 结论 |
| --- | --- | --- |
| `frontend/src/components/settings/CvarDefinitionDetails.vue` | `definition.values` 渲染为纯 `div`，无 `@click`、无选中态、无 emit | 只能看，不能点 |
| `frontend/src/views/ServerConfig.vue` 行内详情 | `:definition="getCustomEntryDefinition(item.entry.command)"`，没有值回写通道 | 点击也无法改命令行 |
| `ServerConfig.vue` 的 `addCatalogEntry` | 固定用 `definition.exampleCommand`（示例值） | 选值无效 |
| `ServerStartup.vue` 的 `addDefinition` | 只调 `createStartupEntry(definition.id)`，值取定义默认值 | 弹窗内无法选值 |
| `frontend/src/data/l4d2Cvars.ts` | 47 条 cvar，`values` / `range` 数据齐备，但数值型没有「常用值」概念 | 需补 UX 元数据 |
| `frontend/src/data/startupOptions.ts` | `kind: select/number/text` 三态，`options` / `min` / `max` 齐备，无 `presets` | 需补 `presets` |

命令行解析能力已存在：`parseCvarCommand`（`l4d2Cvars.ts`）能把 `sm_cvar xxx yyy` 拆成 `{ name, value }`，`utils/startupOptions.ts` 的 `serializeStartupCommand` 负责加引号回写。

## 3. 交互设计

### 3.1 取值卡片（有候选值）

- `values` 存在时可点击（仅管理员可点，游客保持只读）。
- 选中态：`ring-2 ring-sky-500` + 「已选」标记，`aria-pressed` 同步。
- 点击即触发 `select` 事件，值使用 `CvarValueInfo.value` 原文（含引号），保证多值参数 `"pitchfork;shovel"` 原样写回。
- 比较选中态时对值做「去外层引号」归一化，避免 `"0"` 与 `0` 判不相等。

### 3.2 数值参数（有 `range`）

- 展示「常用取值」按钮组，数据来自 `cvarRangePresets`；
- 右侧一个自定义输入框，支持直接填入范围内任意数值；
- **互斥规则**：点击预设 → 清空自定义输入框；在自定义框输入 → 预设高亮立即取消（`customMode` 本地态控制），不会出现「预设 + 自定义」双选；
- 输入非数字或越界 → 就地提示，不写回命令行。

### 3.3 保存落点

- 行内详情：选中值 → 重写该条 `customEntries[i].command` → `syncTextSourceFromEntries()`，仍需点「保存修改」才落 cfg。
- 参数库弹窗：选中值 → `addCatalogEntry` 用「参数名 + 选中值」拼命令，未选值时回退示例值。
- 启动项弹窗：候选值/预设/自定义 → `createStartupEntry(id, token, value)`，添加后重置该行草稿。

## 4. 数据流

```
CvarDefinitionDetails
  props:  definition / selectable / selectedValue
  emits:  select(value)
        │
        ├── ServerConfig 行内  → applyCvarValueToEntry(index, value)
        │                        └─ buildCvarCommand(原命令, value) → syncTextSourceFromEntries()
        └── ServerConfig 参数库 → librarySelectedValue → addCatalogEntry(definition)

ServerStartup「选择启动项」弹窗
  catalogValueDraft[id] = { selected, custom }
  有效值 = number: custom || selected || defaultValue
           select: selected || defaultValue
  → addDefinition(definition) → createStartupEntry(id, undefined, 有效值)
```

## 5. 边界与约束

- `sm_cvar` 前缀保留：原命令带前缀就继续带，避免改写用户写法。
- 游客（`isAdmin === false`）不出现可点击态与自定义输入。
- `hasTrailingComments` 为真时禁止改值（与现有编辑/删除按钮一致）。
- 数值型启动项添加前校验 `min/max`，越界直接拦截并提示。
- 多值参数不做拆分：`;` 分隔的多值按一个完整字符串写入。
- 空值不写回，避免生成 `sm_cvar xxx`（缺值）的坏命令。

## 6. 改动文件

| 文件 | 改动 |
| --- | --- |
| `frontend/src/data/l4d2Cvars.ts` | 新增 `CvarPresetValue`、`cvarRangePresets`、`resolveCvarRangePresets` |
| `frontend/src/components/settings/CvarDefinitionDetails.vue` | 值卡片可点击 + 选中态；数值参数常用值 + 自定义输入（互斥） |
| `frontend/src/views/ServerConfig.vue` | 行内值回写、参数库选值添加、`librarySelectedValue` 状态 |
| `frontend/src/data/startupOptions.ts` | `StartupValueDefinition.presets` + `resolveStartupNumberPresets` |
| `frontend/src/views/ServerStartup.vue` | 选择弹窗内候选值/预设/自定义输入，`addDefinition` 带值添加 |

## 7. 测试计划

1. `npm --prefix frontend run build`（含 `vue-tsc` 类型检查）必须零错误。
2. 参数库：选 `sv_region` → 点「4 亚洲」→ 添加，命令行应为 `sm_cvar sv_region 4`。
3. 行内：`sm_cvar sv_consistency 0` 点「1 开启」，命令行变 `sm_cvar sv_consistency 1`，注释不变。
4. 数值参数：`sv_maxrate` 点预设「30000」→ 命令行变 30000；再在自定义框输入 25000 → 预设高亮消失、值变 25000。
5. 多值：`l4d2_melee_spawn` 选「加入干草叉和铲子」→ 命令行为 `sm_cvar l4d2_melee_spawn "pitchfork;shovel"`。
6. 启动项弹窗：`tickrate` 点「128 tick」→ 添加 → 条目值为 128；`sv_region` 预设「全球 255」→ 添加 → 值 255；自定义输入框输入 7 → 预设取消高亮 → 添加 → 值 7。
7. 越界：`maxplayers` 自定义输入 99 → 添加被拦截并提示。
8. 游客登录：值卡片不可点击、无自定义输入。
9. 端到端：保存后远端 cfg / ExecStart 内容与页面一致。
